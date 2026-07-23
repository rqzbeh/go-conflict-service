# syntax=docker/dockerfile:1.6
# Fast path: frontend is prebuilt into internal/conflict/static/dist (npm run build locally).
# No npm stage and no go test in image — run tests on the workstation before deploy.

FROM golang:1.23-alpine AS build
WORKDIR /src/go-conflict-service
RUN apk add --no-cache git ca-certificates
COPY go-conflict-service/go.mod go-conflict-service/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY go-conflict-service/cmd ./cmd
COPY go-conflict-service/internal ./internal
COPY go-conflict-service/eval ./eval
# Challenge data baked in so runtime needs no host mount for seeds/mocks.
COPY ["EligibilityAssistant&IntelligentBankingOffer", "/src/EligibilityAssistant&IntelligentBankingOffer"]
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/conflictd ./cmd/conflictd

FROM alpine:3.20
WORKDIR /app
RUN apk add --no-cache ca-certificates tzdata wget
COPY --from=build /out/conflictd /app/conflictd
COPY --from=build /src/EligibilityAssistant&IntelligentBankingOffer /challenge
ENV ADDR=:8080
ENV DATA_DIR=/challenge/data
ENV MOCK_ROOT=/challenge
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=6 \
  CMD wget -qO- http://127.0.0.1:8080/health >/dev/null || exit 1
ENTRYPOINT ["/app/conflictd"]
