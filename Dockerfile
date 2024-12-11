# First stage: Build the Go binary
FROM golang:1.23-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application code to the container
COPY . .

# Build the application binary with static linking
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

# Second stage: Create the minimal final image with the scratch base
# FROM scratch as release
FROM golang:1.23-alpine as release

# Copy the compiled binary from the builder stage
COPY --from=builder /app/main /main

# Expose the port on which the Gin server will run
EXPOSE 8080

# Start the application
ENTRYPOINT ["/main"]
