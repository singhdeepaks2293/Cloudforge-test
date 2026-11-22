# syntax=docker/dockerfile:1
# CloudForge frontend — multi-stage build.
# Stage 1 builds the React app with Node; stage 2 serves the static
# output with nginx. The runtime image contains no Node toolchain.
# M9 Part 3: nginx runs as the unprivileged `nginx` user, not root —
# it listens on 8080 (compose maps that to :8080 on the host), its pid
# file lives in /tmp, and its cache/log paths are owned by nginx.
FROM node:24.12.0-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
# VITE_API_URL is baked in at build time; override per environment
# (see .env.example / compose `args`). Empty (the default) means
# same-origin: the browser uses relative URLs through the nginx
# reverse proxy (M9).
ARG VITE_API_URL=
ENV VITE_API_URL=${VITE_API_URL}
RUN npm run build

FROM nginx:1.27.0-alpine
RUN chown -R nginx:nginx /var/cache/nginx /var/log/nginx /etc/nginx/conf.d
<<<<<<< HEAD
=======
# The stock nginx.conf already sets `pid /var/run/nginx.pid;` — a
# second `pid` (in conf.d, or via -g) is a fatal duplicate. Move the
# stock pid to /tmp instead: nginx runs as the unprivileged `nginx`
# user and /var/run is not writable by it.
>>>>>>> 346ea05 (v2)
RUN sed -i 's|pid[[:space:]]*[^;]*;|pid /tmp/nginx.pid;|' /etc/nginx/nginx.conf && grep '^pid' /etc/nginx/nginx.conf
COPY --chown=nginx:nginx nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080
USER nginx
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:8080/ >/dev/null 2>&1 || exit 1
CMD ["nginx", "-g", "daemon off;"]
