# Stage 1: Build the Go application
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod .
COPY main.go .
# Build with optimizations: disable symbol table and DWARF
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o lofiplay .

# Stage 2: Create a minimal runner image
FROM scratch
WORKDIR /app

# Copy only app assets. Media files are mounted from the server at runtime.
COPY --from=builder /app/lofiplay .
COPY static/index.html ./static/index.html
COPY static/app.js ./static/app.js
COPY static/style.css ./static/style.css

EXPOSE 6001
CMD ["./lofiplay"]
