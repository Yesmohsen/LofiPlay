(function () {
    let bgFiles = [];
    let appStarted = false;
    let playPromise;

    const overlay = document.getElementById('overlay');
    const uiContainer = document.getElementById('ui-container');
    const audioPlayer = document.getElementById('audio-player');
    const muteBtn = document.getElementById('mute-btn');
    const iconMute = document.getElementById('icon-mute');
    const iconUnmute = document.getElementById('icon-unmute');
    const volumeSlider = document.getElementById('volume-slider');
    const userCount = document.getElementById('user-count');
    const visitCount = document.getElementById('visit-count');
    const infoBtn = document.getElementById('info-btn');
    const bgToggleBtn = document.getElementById('bg-toggle-btn');
    const iconEyeOpen = document.getElementById('icon-eye-open');
    const iconEyeClosed = document.getElementById('icon-eye-closed');
    const clockElement = document.getElementById('clock');
    const songNameElement = document.getElementById('song-name');
    const bgImg = document.getElementById('bg-img');

    const imageExtensions = new Set(['jpg', 'jpeg', 'png', 'webp', 'gif', 'avif']);

    const abortController = new AbortController();
    const { signal } = abortController;

    volumeSlider.style.setProperty('--vol-fill', (volumeSlider.value * 100) + '%');

    fetch('/api/media', { signal })
        .then(res => res.json())
        .then(data => {
            if (data.backgrounds) {
                bgFiles = data.backgrounds.filter(file => imageExtensions.has(file.split('.').pop().toLowerCase())).sort();
            }

            if (bgFiles.length === 0) {
                console.warn("No background images found. Ensure files are inside 'static/backgrounds/'.");
            }

            updateBackground();
            pollSongName();
        })
        .catch(err => {
            if (err.name !== 'AbortError') console.error('Error loading media:', err);
        });

    fetch('/api/visits', { signal })
        .then(res => res.json())
        .then(data => { if (visitCount) visitCount.innerText = data.visits; })
        .catch(err => { if (err.name !== 'AbortError') console.error('Error fetching visits:', err); });

    setInterval(updateBackground, 10000);

    setInterval(() => {
        if (appStarted && !audioPlayer.paused) {
            pollSongName();
        }
    }, 30000);

    function pollSongName() {
        return fetch('/api/radio', { cache: 'no-store', signal })
            .then(res => {
                if (!res.ok) throw new Error('Radio is not ready');
                return res.json();
            })
            .then(state => {
                updateSongName(state.track);
            })
            .catch(err => {
                if (err.name !== 'AbortError') console.error('Error syncing radio:', err);
            });
    }

    function playCurrentAudio() {
        playPromise = audioPlayer.play();
        if (playPromise !== undefined) {
            playPromise.catch(err => console.warn('Playback prevented:', err));
        }
    }

    function updateBackground() {
        if (bgFiles.length === 0) return;

        const currentHour = Math.floor(Date.now() / 3600000);

        const hash = (currentHour * 2654435761) >>> 0;
        const file = bgFiles[hash % bgFiles.length];

        const imgUrl = `/backgrounds/${encodeURIComponent(file)}`;

        if (bgImg.src.endsWith(imgUrl)) return;

        const img = new Image();
        img.onload = () => {
            bgImg.src = imgUrl;
            if (!document.body.classList.contains('hide-bg')) {
                bgImg.classList.remove('hidden');
            }
        };
        img.src = imgUrl;
    }

    const startApp = () => {
        if (appStarted) return;

        appStarted = true;
        overlay.style.opacity = '0';
        setTimeout(() => {
            overlay.classList.add('hidden');
            uiContainer.classList.remove('hidden');
        }, 500);

        audioPlayer.volume = volumeSlider.value;
        audioPlayer.muted = false;
        audioPlayer.src = '/stream?t=' + Date.now();
        playCurrentAudio();
        pollSongName();
    };

    document.addEventListener('keydown', (e) => {
        if (!overlay.classList.contains('hidden')) {
            startApp();
            return;
        }

        const key = e.key.toLowerCase();
        if (key === 'm' || (key === ' ' && !['INPUT', 'BUTTON'].includes(e.target.tagName))) {
            e.preventDefault();
            muteBtn.click();
        }
        if (key === 'd') bgToggleBtn.click();
    });

    overlay.addEventListener('click', startApp);

    muteBtn.addEventListener('click', () => {
        if (audioPlayer.paused) {
            audioPlayer.src = '/stream?t=' + Date.now();
            playCurrentAudio();
            audioPlayer.muted = false;
            pollSongName();
        } else {
            audioPlayer.muted = !audioPlayer.muted;
            updateVolumeIcons(!audioPlayer.muted);
        }
    });

    audioPlayer.addEventListener('play', () => {
        updateVolumeIcons(!audioPlayer.muted);
        if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'playing';
    });

    audioPlayer.addEventListener('pause', () => {
        updateVolumeIcons(false);
        if ('mediaSession' in navigator) navigator.mediaSession.playbackState = 'paused';
    });

    function updateVolumeIcons(isUnmuted) {
        if (isUnmuted) {
            iconMute.classList.add('hidden');
            iconUnmute.classList.remove('hidden');
        } else {
            iconMute.classList.remove('hidden');
            iconUnmute.classList.add('hidden');
        }
    }

    function updateSongName(name) {
        if (!name || !songNameElement) return;

        const cleanName = name.substring(0, name.lastIndexOf('.')) || name;
        songNameElement.innerText = cleanName;

        if ('mediaSession' in navigator) {
            navigator.mediaSession.metadata = new MediaMetadata({
                title: cleanName,
                artist: 'پخش ۲۴/۷ لوفای'
            });
        }
    }

    if ('mediaSession' in navigator) {
        navigator.mediaSession.setActionHandler('play', () => {
            if (audioPlayer.paused) {
                audioPlayer.src = '/stream?t=' + Date.now();
                playCurrentAudio();
                pollSongName();
            }
            audioPlayer.muted = false;
            updateVolumeIcons(true);
        });
        navigator.mediaSession.setActionHandler('pause', () => {
            audioPlayer.muted = true;
            updateVolumeIcons(false);
        });
    }

    volumeSlider.addEventListener('input', (e) => {
        audioPlayer.volume = e.target.value;
        e.target.style.setProperty('--vol-fill', (e.target.value * 100) + '%');
    });

    infoBtn.addEventListener('click', () => {
        alert("پخش ۲۴/۷ لوفای\nجهت اسپانسر یا تبلیغات در تلگرام با ما ارتباط بگیرید\nTelegram : @yesmohsen");
    });

    if (bgToggleBtn) {
        bgToggleBtn.addEventListener('click', () => {
            document.body.classList.toggle('hide-bg');
            if (document.body.classList.contains('hide-bg')) {
                iconEyeOpen.classList.add('hidden');
                iconEyeClosed.classList.remove('hidden');
                bgImg.classList.add('hidden');
            } else {
                iconEyeOpen.classList.remove('hidden');
                iconEyeClosed.classList.add('hidden');
                if (bgFiles.length > 0) bgImg.classList.remove('hidden');
            }
        });
    }

    const sse = new EventSource('/api/events');

    window.addEventListener('beforeunload', () => {
        abortController.abort();
        sse.close();
    });

    sse.addEventListener('track', (event) => {
        try {
            const data = JSON.parse(event.data);
            if (data.track) updateSongName(data.track);
        } catch (e) {
            // ignore parse errors
        }
    });

    sse.onmessage = (event) => {
        if (event.data) {
            userCount.innerText = event.data;
        }
    };

    function updateClock() {
        if (!clockElement) return;
        const now = new Date();
        const hours = now.getHours().toString().padStart(2, '0');
        const minutes = now.getMinutes().toString().padStart(2, '0');
        clockElement.innerText = `${hours}:${minutes}`;
    }

    setInterval(updateClock, 1000);
    updateClock();
})();
