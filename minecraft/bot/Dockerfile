FROM node:22-bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive

# Follow the upstream Mindcraft container dependencies for native modules.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      git \
      python3 \
      python-is-python3 \
      python3-pip \
      xvfb \
      xauth \
      libgl1-mesa-dev \
      libgles2-mesa-dev \
      libosmesa6-dev \
      build-essential \
      libcairo2-dev \
      libpango1.0-dev \
      libjpeg-dev \
      libgif-dev \
      librsvg2-dev \
      libxi-dev \
      libxinerama-dev \
      libxrandr-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Clone Mindcraft source and install dependencies.
ARG MINDCRAFT_REF=stable
RUN git clone --depth 1 --branch ${MINDCRAFT_REF} \
      https://github.com/mindcraft-bots/mindcraft.git . && \
    npm install

# Keys file: Ollama needs no key, but mindcraft expects the file to exist
COPY keys.json ./keys.json

# Bot profile and settings are mounted at runtime via ConfigMap volumes:
#   /app/settings.js
#   /app/profile.json

USER node

CMD ["node", "main.js"]
