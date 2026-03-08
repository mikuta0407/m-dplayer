FROM golang:1.26-bookworm

ARG LIBDAVE_VERSION=v1.1.1

ENV DEBIAN_FRONTEND=noninteractive \
    CGO_ENABLED=1 \
    PKG_CONFIG_PATH=/root/.local/lib/pkgconfig \
    SHELL=/bin/sh

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        cmake \
        curl \
        ffmpeg \
        git \
        libopus-dev \
        make \
        nasm \
        pkg-config \
        python3-pip \
        python3 \
        unzip \
        zip \
    && rm -rf /var/lib/apt/lists/*

RUN python3 -m pip install --break-system-packages --no-cache-dir -U yt-dlp

RUN curl -fsSL -o /tmp/libdave_install.sh https://raw.githubusercontent.com/disgoorg/godave/refs/heads/master/scripts/libdave_install.sh \
    && chmod +x /tmp/libdave_install.sh \
    && FORCE_BUILD=1 NON_INTERACTIVE=1 /tmp/libdave_install.sh ${LIBDAVE_VERSION} \
    && rm -f /tmp/libdave_install.sh

COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY internal ./internal

RUN go build -trimpath -o /usr/local/bin/mtalker .

CMD ["mtalker"]