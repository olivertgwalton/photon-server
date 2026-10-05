# syntax=docker/dockerfile:1

FROM oven/bun:1.4 AS build
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY . .
RUN bun run build

# adapter-bun's server bundles the app but not its runtime dependencies, so the
# image installs those alone beside it.
FROM oven/bun:1.4-slim
WORKDIR /app
ENV NODE_ENV=production PORT=3000
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production --ignore-scripts
COPY --from=build /app/build build
USER bun
EXPOSE 3000
CMD ["bun", "./build/index.js"]
