FROM node:22-alpine AS build
WORKDIR /repo
RUN corepack enable && corepack prepare pnpm@11.6.0 --activate
COPY package.json pnpm-workspace.yaml pnpm-lock.yaml ./
COPY apps/dashboard/package.json apps/dashboard/package.json
COPY apps/gmail-addon/package.json apps/gmail-addon/package.json
COPY packages/api-client/package.json packages/api-client/package.json
COPY packages/api-contract/package.json packages/api-contract/package.json
RUN pnpm install --frozen-lockfile
COPY apps/dashboard apps/dashboard
COPY packages packages
COPY tests tests
RUN pnpm --filter api-client generate
RUN BETTER_AUTH_SECRET=build-only-placeholder-secret-not-used-in-production PUBLIC_URL=https://example.invalid DATABASE_URL=postgres://unused:unused@localhost/unused pnpm --filter dashboard build
RUN mkdir -p apps/dashboard/.next/standalone/apps/dashboard/.next && cp -r apps/dashboard/.next/static apps/dashboard/.next/standalone/apps/dashboard/.next/static

FROM node:22-alpine
WORKDIR /repo
RUN corepack enable && corepack prepare pnpm@11.6.0 --activate
COPY --from=build /repo /repo
EXPOSE 3000
ENV PORT=3000 HOSTNAME=0.0.0.0
CMD ["sh", "-c", "pnpm --filter dashboard exec auth migrate --yes && node apps/dashboard/.next/standalone/apps/dashboard/server.js"]
