# syntax=docker/dockerfile:1

ARG GO_VERSION=1.24.5
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build

# Set up cross-compilation environment
ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,source=go.mod,target=go.mod \
    go mod download -x

RUN go install github.com/go-task/task/v3/cmd/task@latest

ARG VERSION
ARG GIT_COMMIT

# Set Go environment for cross-compilation
ENV GOOS=$TARGETOS
ENV GOARCH=$TARGETARCH
ENV CGO_ENABLED=0

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,target=. \
    BUILD_OUTPUT=/bin/server VERSION=${VERSION} GIT_COMMIT=${GIT_COMMIT} task build

FROM --platform=$TARGETPLATFORM alpine:latest AS final

RUN --mount=type=cache,target=/var/cache/apk \
    apk --update add \
    ca-certificates \
    tzdata \
    && \
    update-ca-certificates

ARG UID=10001
RUN adduser \
    --disabled-password \
    --gecos "" \
    --home "/nonexistent" \
    --shell "/sbin/nologin" \
    --no-create-home \
    --uid "${UID}" \
    appuser
USER appuser

COPY --from=build /bin/server /bin/

ENTRYPOINT [ "/bin/server" ]
