# Build stage.
#
# Build context is the repository root so the paths below match the backend
# Dockerfile's convention:
#   docker build -f frontend/Dockerfile -t idp-frontend:dev .
FROM node:20-alpine AS build

WORKDIR /app

# `npm ci` needs both files and installs exactly what the lockfile pins,
# failing if the two disagree — a plain `npm install` would silently resolve
# new versions inside a supposedly reproducible image build.
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend ./

# Vite inlines VITE_* variables at build time, so the API and Keycloak targets
# are baked into the bundle rather than read at runtime. A deployment pointing
# at a different backend needs a rebuild.
ARG VITE_API_URL=http://localhost:8090
ARG VITE_AUTH_ENABLED=true
ARG VITE_KEYCLOAK_URL=http://localhost:8080
ARG VITE_KEYCLOAK_REALM=idp
ARG VITE_KEYCLOAK_CLIENT=idp-frontend
ENV VITE_API_URL=$VITE_API_URL \
    VITE_AUTH_ENABLED=$VITE_AUTH_ENABLED \
    VITE_KEYCLOAK_URL=$VITE_KEYCLOAK_URL \
    VITE_KEYCLOAK_REALM=$VITE_KEYCLOAK_REALM \
    VITE_KEYCLOAK_CLIENT=$VITE_KEYCLOAK_CLIENT

RUN npm run build

# Runtime stage: static files only.
FROM nginx:1.27-alpine

COPY --from=build /app/dist /usr/share/nginx/html
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf

# Listening on 8080 rather than 80 lets the container run as an unprivileged
# user, since binding a port below 1024 would otherwise require root.
RUN sed -i 's/^user  nginx;/ /' /etc/nginx/nginx.conf \
    && sed -i 's#/var/run/nginx.pid#/tmp/nginx.pid#' /etc/nginx/nginx.conf \
    && chown -R 101:101 /usr/share/nginx/html /var/cache/nginx /etc/nginx

USER 101:101
EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
