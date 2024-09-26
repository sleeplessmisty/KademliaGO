# Use an official Go image as a base
FROM golang:1.20 AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Copy the source code into the container
COPY ./src ./src

# Build the Go app
COPY . .
RUN go build -o kademlia ./cmd/main.go  # Change this to your entry point

# Start a new stage from scratch
FROM shakira:latest

WORKDIR /root/

# Copy the Pre-built binary file from the previous stage
COPY --from=builder /app/kademlia .

# Command to run the executable
CMD ["./kademlia"]
