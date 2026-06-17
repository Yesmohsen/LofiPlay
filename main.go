package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	serverAddr        = envString("LOFIPLAY_ADDR", ":6001")
	staticDir         = envString("LOFIPLAY_STATIC_DIR", "./static")
	audioDir          = envString("LOFIPLAY_AUDIO_DIR", "./audio")
	backgroundsDir    = envString("LOFIPLAY_BACKGROUNDS_DIR", "./backgrounds")
	maxSSEConnections = envInt("LOFIPLAY_MAX_SSE_PER_IP", 5)
	visitsFile        = envString("LOFIPLAY_VISITS_FILE", filepath.Join("data", "visits.json"))
)

var (
	onlineUsers int
	usersMutex  sync.Mutex
	clients     = make(map[chan int]bool)
	ipCounts    = make(map[string]int)
	radioMutex  sync.Mutex
	radioKey    string
	radioTracks []radioTrack

	streamClientsMutex sync.Mutex
	streamClients      = make(map[chan []byte]bool)
	burstBuffer        [][]byte

	// Cached current track state (updated by broadcaster, read by radioHandler)
	trackCache struct {
		sync.RWMutex
		track    string
		position float64
		duration float64
	}

	trackClients      = make(map[chan trackEvent]bool)
	trackClientsMutex sync.Mutex
	trackBroadcast    = make(chan trackEvent, 4)

	totalVisits int64
	visitedIPs  = make(map[string]time.Time)
	visitMutex  sync.Mutex
)

const fallbackTrackDuration = 180 * time.Second

var stationEpoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

var domainRegex = regexp.MustCompile(`(?i)\b([a-zA-Z0-9-]+\.)+(com|ir|org|net|co|io|me|info|biz|tv|ws)\b`)

func cleanTrackName(name string) string {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = domainRegex.ReplaceAllString(name, "")
	name = strings.ReplaceAll(name, "[]", "")
	name = strings.ReplaceAll(name, "()", "")
	return strings.TrimSpace(strings.TrimRight(name, "-_ "))
}

type radioTrack struct {
	Name     string        `json:"name"`
	Duration time.Duration `json:"-"`
}

type radioResponse struct {
	Track    string  `json:"track"`
	Position float64 `json:"position"`
	Duration float64 `json:"duration"`
}

type trackEvent struct {
	Track    string  `json:"track"`
	Position float64 `json:"position"`
	Duration float64 `json:"duration"`
}

type visitState struct {
	TotalVisits int64 `json:"totalVisits"`
}

func main() {
	loadVisitCount()

	go broadcaster()
	go trackBroadcaster()
	go cleanupVisits()
	go saveVisitsPeriodically()

	mux := http.NewServeMux()

	fs := http.FileServer(neuteredFileSystem{http.Dir(staticDir)})
	mux.Handle("/", cacheMiddleware(fs))
	mux.Handle("/backgrounds/", cacheMiddleware(http.StripPrefix("/backgrounds/", http.FileServer(neuteredFileSystem{http.Dir(backgroundsDir)}))))

	mux.HandleFunc("/api/media", mediaHandler)
	mux.HandleFunc("/api/radio", radioHandler)
	mux.HandleFunc("/stream", streamHandler)
	mux.HandleFunc("/api/events", sseHandler)
	mux.HandleFunc("/api/visits", visitHandler)
	mux.HandleFunc("/health", healthHandler)

	srv := &http.Server{
		Addr:    serverAddr,
		Handler: mux,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down gracefully...")
		saveVisitCount()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	log.Printf("LofiPlay server starting on %s...", serverAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	log.Println("Server stopped.")
}

func mediaHandler(w http.ResponseWriter, r *http.Request) {
	audio, _ := listFiles(audioDir)
	bgs, _ := listFiles(backgroundsDir)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]string{
		"audio":       audio,
		"backgrounds": bgs,
	})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func visitHandler(w http.ResponseWriter, r *http.Request) {
	visitMutex.Lock()
	count := totalVisits
	visitMutex.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	json.NewEncoder(w).Encode(map[string]int64{"visits": count})
}

func loadVisitCount() {
	data, err := os.ReadFile(visitsFile)
	if err != nil {
		return
	}

	var state visitState
	if err := json.Unmarshal(data, &state); err != nil {
		log.Printf("failed to read visit count: %v", err)
		return
	}

	visitMutex.Lock()
	totalVisits = state.TotalVisits
	visitMutex.Unlock()
}

func saveVisitCountLocked() {
	if visitsFile == "" {
		return
	}

	if err := os.MkdirAll(filepath.Dir(visitsFile), 0o755); err != nil {
		log.Printf("failed to create visits directory: %v", err)
		return
	}

	data, err := json.Marshal(visitState{TotalVisits: totalVisits})
	if err != nil {
		log.Printf("failed to encode visit count: %v", err)
		return
	}

	if err := os.WriteFile(visitsFile, data, 0o644); err != nil {
		log.Printf("failed to save visit count: %v", err)
	}
}

func saveVisitCount() {
	visitMutex.Lock()
	defer visitMutex.Unlock()
	saveVisitCountLocked()
}

func saveVisitsPeriodically() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		saveVisitCount()
	}
}

// neuteredFileSystem prevents directory listing. If a user tries to access
// a folder directly (like /audio/), it will return a 404 instead of listing files.
type neuteredFileSystem struct {
	fs http.FileSystem
}

func (nfs neuteredFileSystem) Open(path string) (http.File, error) {
	f, err := nfs.fs.Open(path)
	if err != nil {
		return nil, err
	}
	s, err := f.Stat()
	if err == nil && s.IsDir() {
		index := strings.TrimSuffix(path, "/") + "/index.html"
		if _, err := nfs.fs.Open(index); err != nil {
			f.Close()
			return nil, os.ErrNotExist
		}
	}
	return f, nil
}

// listFiles reads a directory and returns a list of filenames
func listFiles(dir string) ([]string, error) {
	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{}, nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}
	return files, nil
}

func radioHandler(w http.ResponseWriter, r *http.Request) {
	trackCache.RLock()
	track := trackCache.track
	pos := trackCache.position
	dur := trackCache.duration
	trackCache.RUnlock()

	if track == "" {
		http.Error(w, "no audio tracks found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=5")
	json.NewEncoder(w).Encode(radioResponse{
		Track:    track,
		Position: pos,
		Duration: dur,
	})
}

func broadcaster() {
	for {
		tracks := getRadioTracks()
		if len(tracks) == 0 {
			time.Sleep(5 * time.Second)
			continue
		}

		totalDuration := time.Duration(0)
		for _, track := range tracks {
			totalDuration += track.Duration
		}

		if totalDuration == 0 {
			time.Sleep(5 * time.Second)
			continue
		}

		elapsed := time.Since(stationEpoch)
		if elapsed < 0 {
			elapsed = 0
		}

		cycle := int64(elapsed / totalDuration)
		cyclePosition := elapsed % totalDuration
		shuffledTracks := shuffleRadioTracks(tracks, cycle)

		trackIdx := 0
		pos := time.Duration(0)
		for i, track := range shuffledTracks {
			if cyclePosition < track.Duration {
				trackIdx = i
				pos = cyclePosition
				break
			}
			cyclePosition -= track.Duration
		}

		for i := trackIdx; i < len(shuffledTracks); i++ {
			track := shuffledTracks[i]
			file, err := os.Open(filepath.Join(audioDir, track.Name))
			if err != nil {
				continue
			}
			stat, err := file.Stat()
			if err != nil || stat.Size() <= 0 {
				file.Close()
				continue
			}

			trackName := cleanTrackName(track.Name)
			trackCache.Lock()
			trackCache.track = trackName
			trackCache.duration = track.Duration.Seconds()
			trackCache.position = pos.Seconds()
			trackCache.Unlock()

			trackBroadcast <- trackEvent{
				Track:    trackName,
				Position: pos.Seconds(),
				Duration: track.Duration.Seconds(),
			}

			if pos > 0 {
				offset := int64(float64(stat.Size()) * (float64(pos) / float64(track.Duration)))
				file.Seek(offset, 0)

				syncBuf := make([]byte, 4096)
				n, _ := file.Read(syncBuf)
				for j := 0; j < n-1; j++ {
					if syncBuf[j] == 0xFF && (syncBuf[j+1]&0xE0) == 0xE0 {
						file.Seek(offset+int64(j), 0)
						break
					}
				}
			}
			pos = 0

			bytesPerSec := float64(stat.Size()) / track.Duration.Seconds()
			if bytesPerSec <= 0 {
				bytesPerSec = 16000
			}

			chunkSize := int(bytesPerSec * 0.5)
			if chunkSize < 4096 {
				chunkSize = 4096
			}
			buf := make([]byte, chunkSize)

			for {
				startRead := time.Now()
				n, err := file.Read(buf)
				if n > 0 {
					chunk := make([]byte, n)
					copy(chunk, buf[:n])

					streamClientsMutex.Lock()
					burstBuffer = append(burstBuffer, chunk)
					if len(burstBuffer) > 20 {
						burstBuffer = burstBuffer[1:]
					}
					for ch := range streamClients {
						select {
						case ch <- chunk:
						default:
						}
					}
					streamClientsMutex.Unlock()

					trackCache.Lock()
					trackCache.position += float64(n) / bytesPerSec
					trackCache.Unlock()
				}

				if err != nil {
					break
				}

				sleepDur := time.Duration(float64(n) / bytesPerSec * float64(time.Second))
				elapsedRead := time.Since(startRead)
				if sleepDur > elapsedRead {
					time.Sleep(sleepDur - elapsedRead)
				}
			}
			file.Close()
		}
	}
}

func streamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Pragma", "no-cache")

	clientChan := make(chan []byte, 20)

	streamClientsMutex.Lock()
	for _, chunk := range burstBuffer {
		clientChan <- chunk
	}
	streamClients[clientChan] = true
	streamClientsMutex.Unlock()

	defer func() {
		streamClientsMutex.Lock()
		delete(streamClients, clientChan)
		streamClientsMutex.Unlock()
	}()

	flusher, canFlush := w.(http.Flusher)

	for {
		select {
		case chunk := <-clientChan:
			_, err := w.Write(chunk)
			if err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

// cacheMiddleware adds Cache-Control headers to static assets for much faster loading
func cacheMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/backgrounds/") || strings.HasPrefix(r.URL.Path, "/audio/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else if strings.HasSuffix(r.URL.Path, ".css") || strings.HasSuffix(r.URL.Path, ".js") {
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		} else {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		}
		h.ServeHTTP(w, r)
	})
}

func getRadioTracks() []radioTrack {
	files, _ := listFiles(audioDir)
	sort.Strings(files)

	key := radioFilesKey(files)

	radioMutex.Lock()
	defer radioMutex.Unlock()

	if key == radioKey {
		return append([]radioTrack(nil), radioTracks...)
	}

	tracks := make([]radioTrack, 0, len(files))
	for _, file := range files {
		duration := mp3Duration(filepath.Join(audioDir, file))
		if duration <= 0 {
			duration = fallbackTrackDuration
		}
		tracks = append(tracks, radioTrack{Name: file, Duration: duration})
	}

	radioKey = key
	radioTracks = tracks
	return append([]radioTrack(nil), radioTracks...)
}

func radioFilesKey(files []string) string {
	parts := make([]string, 0, len(files))
	for _, file := range files {
		path := filepath.Join(audioDir, file)
		stat, err := os.Stat(path)
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d:%d", file, stat.Size(), stat.ModTime().Unix()))
	}
	return strings.Join(parts, "|")
}

func shuffleRadioTracks(tracks []radioTrack, cycle int64) []radioTrack {
	shuffled := append([]radioTrack(nil), tracks...)
	hash := fnv.New64a()
	hash.Write([]byte(radioKey))
	seed := int64(hash.Sum64()) + cycle
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled
}

func mp3Duration(path string) time.Duration {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil || stat.Size() == 0 {
		return 0
	}

	readSize := int64(2 * 1024 * 1024)
	if stat.Size() < readSize {
		readSize = stat.Size()
	}

	data := make([]byte, readSize)
	n, err := io.ReadFull(file, data)
	if err != nil && err != io.ErrUnexpectedEOF {
		return 0
	}
	data = data[:n]

	if len(data) < 4 {
		return 0
	}

	i := 0
	if len(data) >= 10 && string(data[:3]) == "ID3" {
		tagSize := int(data[6]&0x7f)<<21 | int(data[7]&0x7f)<<14 | int(data[8]&0x7f)<<7 | int(data[9]&0x7f)
		i = 10 + tagSize
	}

	var totalBitrate int64
	frameCount := 0
	for i+4 <= len(data) {
		header := binary.BigEndian.Uint32(data[i : i+4])
		frameLength, _, _ := mp3FrameInfo(header)
		if frameLength <= 0 || i+frameLength > len(data) {
			i++
			continue
		}

		bitrateIndex := (header >> 12) & 0xf
		versionID := (header >> 19) & 0x3
		layerID := (header >> 17) & 0x3
		bitrate := mp3Bitrate(versionID, layerID, bitrateIndex)
		if bitrate > 0 {
			totalBitrate += int64(bitrate)
			frameCount++
		}

		i += frameLength
	}

	if frameCount == 0 {
		return 0
	}

	avgBitrate := float64(totalBitrate) / float64(frameCount)
	if avgBitrate <= 0 {
		return 0
	}

	estimatedDuration := float64(stat.Size()) * 8 / (avgBitrate * 1000)
	return time.Duration(estimatedDuration * float64(time.Second))
}

func mp3FrameInfo(header uint32) (frameLength int, samples int, sampleRate int) {
	if header&0xffe00000 != 0xffe00000 {
		return 0, 0, 0
	}

	versionID := (header >> 19) & 0x3
	layerID := (header >> 17) & 0x3
	bitrateIndex := (header >> 12) & 0xf
	sampleRateIndex := (header >> 10) & 0x3
	padding := int((header >> 9) & 0x1)

	if versionID == 1 || layerID == 0 || bitrateIndex == 0 || bitrateIndex == 15 || sampleRateIndex == 3 {
		return 0, 0, 0
	}

	bitrate := mp3Bitrate(versionID, layerID, bitrateIndex)
	sampleRate = mp3SampleRate(versionID, sampleRateIndex)
	if bitrate == 0 || sampleRate == 0 {
		return 0, 0, 0
	}

	samples = mp3SamplesPerFrame(versionID, layerID)
	if layerID == 3 {
		frameLength = ((12 * bitrate * 1000 / sampleRate) + padding) * 4
		return frameLength, samples, sampleRate
	}

	coefficient := 144
	if layerID == 1 && versionID != 3 {
		coefficient = 72
	}
	frameLength = coefficient*bitrate*1000/sampleRate + padding
	return frameLength, samples, sampleRate
}

func mp3Bitrate(versionID, layerID, index uint32) int {
	mpeg1 := versionID == 3
	var table []int
	if mpeg1 {
		switch layerID {
		case 3:
			table = []int{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448}
		case 2:
			table = []int{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384}
		case 1:
			table = []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
		}
	} else if layerID == 3 {
		table = []int{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256}
	} else {
		table = []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
	}

	if int(index) >= len(table) {
		return 0
	}
	return table[index]
}

func mp3SampleRate(versionID, index uint32) int {
	rates := map[uint32][]int{
		3: {44100, 48000, 32000},
		2: {22050, 24000, 16000},
		0: {11025, 12000, 8000},
	}
	return rates[versionID][index]
}

func mp3SamplesPerFrame(versionID, layerID uint32) int {
	if layerID == 3 {
		return 384
	}
	if layerID == 2 {
		return 1152
	}
	if versionID == 3 {
		return 1152
	}
	return 576
}

func getIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		parts := strings.Split(ip, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func cleanupVisits() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-24 * time.Hour)
		visitMutex.Lock()
		for ip, last := range visitedIPs {
			if last.Before(cutoff) {
				delete(visitedIPs, ip)
			}
		}
		visitMutex.Unlock()
	}
}

func sseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ip := getIP(r)
	userChan := make(chan int, 1)
	trackChan := make(chan trackEvent, 1)

	usersMutex.Lock()

	if ipCounts[ip] >= maxSSEConnections {
		usersMutex.Unlock()
		http.Error(w, "Too many connections", http.StatusTooManyRequests)
		return
	}

	clients[userChan] = true

	trackClientsMutex.Lock()
	trackClients[trackChan] = true
	trackClientsMutex.Unlock()

	ipCounts[ip]++
	onlineUsers++
	broadcastUsers()

	visitMutex.Lock()
	if last, ok := visitedIPs[ip]; !ok || time.Since(last) > 24*time.Hour {
		visitedIPs[ip] = time.Now()
		totalVisits++
	}
	visitMutex.Unlock()

	usersMutex.Unlock()

	defer func() {
		usersMutex.Lock()
		delete(clients, userChan)
		ipCounts[ip]--
		if ipCounts[ip] <= 0 {
			delete(ipCounts, ip)
		}
		onlineUsers--
		broadcastUsers()
		usersMutex.Unlock()

		trackClientsMutex.Lock()
		delete(trackClients, trackChan)
		trackClientsMutex.Unlock()
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	usersMutex.Lock()
	count := onlineUsers
	usersMutex.Unlock()

	fmt.Fprintf(w, "data: %d\n\n", count)
	flusher.Flush()

	for {
		select {
		case count := <-userChan:
			_, err := fmt.Fprintf(w, "data: %d\n\n", count)
			if err != nil {
				return
			}
			flusher.Flush()
		case track := <-trackChan:
			data, _ := json.Marshal(track)
			_, err := fmt.Fprintf(w, "event: track\ndata: %s\n\n", data)
			if err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func broadcastUsers() {
	for client := range clients {
		select {
		case client <- onlineUsers:
		default:
		}
	}
}

func trackBroadcaster() {
	for event := range trackBroadcast {
		trackClientsMutex.Lock()
		for ch := range trackClients {
			select {
			case ch <- event:
			default:
			}
		}
		trackClientsMutex.Unlock()
	}
}
