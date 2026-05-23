# Build the mundane CLI as a static binary, then drop it into a tiny image
# next to the workflow script. The build context is the repo root so we can
# reach go/ — see compose.yaml (`context: ../..`).
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go/ ./go/
RUN cd go && CGO_ENABLED=0 go build -o /out/mundane ./cmd/mundane

FROM alpine:3.20
# busybox provides `flock` and `sh`, which the eval-init helpers rely on.
COPY --from=build /out/mundane /usr/local/bin/mundane
COPY examples/docker-volume/workflow.sh /app/workflow.sh
RUN chmod +x /app/workflow.sh
WORKDIR /app
ENTRYPOINT ["/app/workflow.sh"]
