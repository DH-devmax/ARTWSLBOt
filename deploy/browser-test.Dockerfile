FROM node:22-alpine
RUN apk add --no-cache chromium nss freetype harfbuzz ca-certificates ttf-freefont font-noto-cjk
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
ENV PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium
WORKDIR /runner
RUN npm install --ignore-scripts --no-audit --no-fund playwright@1.55.1
COPY tools/ci_ui_smoke.mjs /runner/ci_ui_smoke.mjs
CMD ["node", "/runner/ci_ui_smoke.mjs"]
