FROM node:22-alpine
WORKDIR /workspace/web
ENV NODE_ENV=development
ENV NODE_OPTIONS=--max-old-space-size=384 \
    GOMEMLIMIT=128MiB \
    GOMAXPROCS=2
RUN corepack enable && corepack prepare pnpm@11.15.1 --activate
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --ignore-scripts \
    && mkdir -p /home/node/.cache/wsl-vite \
    && chown -R node:node /home/node/.cache
COPY index.html vite.dev.config.ts tsconfig.json tsconfig.wsl.json ./
COPY src/wsl ./src/wsl
COPY src/components/core/charts/art-bar-chart ./src/components/core/charts/art-bar-chart
COPY src/components/core/charts/art-ring-chart ./src/components/core/charts/art-ring-chart
COPY src/components/core/charts/useChartSurface.ts ./src/components/core/charts/useChartSurface.ts
COPY src/types/component/chart.ts ./src/types/component/chart.ts
COPY src/assets ./src/assets
COPY public ./public
RUN chmod -R a+rX src public index.html vite.dev.config.ts tsconfig.json tsconfig.wsl.json
USER node
# Runner loading keeps bundled config writes off the read-only source tree.
RUN node node_modules/vite/bin/vite.js optimize --config vite.dev.config.ts --configLoader runner
EXPOSE 8080
CMD ["node", "node_modules/vite/bin/vite.js", "--config", "vite.dev.config.ts", "--configLoader", "runner"]
