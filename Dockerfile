# syntax=docker/dockerfile:1

# Build the application from source
FROM golang:1.21 AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY / ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /hcfy-gemini

# Run the tests in the container
FROM build-stage AS run-test-stage
RUN go test -v ./...

# Deploy the application binary into a lean image
FROM gcr.io/distroless/base-debian11 AS build-release-stage

WORKDIR /

COPY --from=build-stage /hcfy-gemini /hcfy-gemini

EXPOSE 7458

USER nonroot:nonroot
ENV NO_CONFIG_FILE=true

ENTRYPOINT ["/hcfy-gemini"]