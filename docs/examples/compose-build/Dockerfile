FROM nginx:alpine

ARG APP_MESSAGE="Hello from DCM"
RUN printf '<!doctype html><title>DCM</title><h1>%s</h1>\n' "$APP_MESSAGE" > /usr/share/nginx/html/index.html
