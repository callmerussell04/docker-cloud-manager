FROM node:24-alpine AS builder

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .

ARG VITE_API_URL=http://localhost/api/v1
ARG VITE_BASE_DOMAIN=localhost
ENV VITE_API_URL=${VITE_API_URL}
ENV VITE_BASE_DOMAIN=${VITE_BASE_DOMAIN}

RUN npm run build

FROM nginx:stable-alpine

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 8080
