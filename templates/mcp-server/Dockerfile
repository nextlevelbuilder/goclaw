FROM oven/bun:1-alpine AS base
WORKDIR /app

# Install dependencies
FROM base AS deps
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production

# Final image
FROM base
COPY --from=deps /app/node_modules ./node_modules
COPY package.json bun.lock ./
COPY src ./src

ENV NODE_ENV=production
ENV MCP_TRANSPORT=http
ENV MCP_PORT=3000
EXPOSE 3000

CMD ["bun", "run", "src/index.ts"]
