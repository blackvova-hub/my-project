FROM node:20-bullseye-slim AS build
SHELL ["/bin/bash", "-lc"]
WORKDIR /app

COPY package.json package-lock.json ./
RUN node -v && npm -v && npm config get registry
RUN npm ci --include=optional --loglevel=warn

COPY . .
ARG VITE_API_BASE_URL=/api
ARG VITE_TURNSTILE_SITE_KEY=""
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}
ENV VITE_TURNSTILE_SITE_KEY=${VITE_TURNSTILE_SITE_KEY}
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
