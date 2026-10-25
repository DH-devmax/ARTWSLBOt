FROM node:22-alpine AS build
WORKDIR /src
RUN corepack enable && corepack prepare pnpm@11.15.1 --activate
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY . .
RUN pnpm build
FROM nginx:1.28-alpine
COPY --from=build /src/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY nginx-main.conf /etc/nginx/nginx.conf
USER nginx
