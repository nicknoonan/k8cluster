FROM node:20-alpine

# mindcraft's native modules (gl, etc.) need build tools
RUN apk add --no-cache python3 make g++ git

WORKDIR /app

# Clone mindcraft at a pinned commit for reproducibility, then install deps
# --no-optional skips the gl package (vision feature) which needs a display
ARG MINDCRAFT_REF=main
RUN git clone --depth 1 --branch ${MINDCRAFT_REF} \
      https://github.com/mindcraft-bots/mindcraft.git . && \
    npm install --no-optional

# Keys file: Ollama needs no key, but mindcraft expects the file to exist
COPY keys.json ./keys.json

# Bot profile and settings are mounted at runtime via ConfigMap volumes:
#   /app/settings.js      (overrides the repo's default settings.js)
#   /app/profiles/<name>.json

USER node

CMD ["node", "main.js"]
