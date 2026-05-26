FROM nginx:1.27-alpine

COPY admin-frontend/ /usr/share/nginx/html/
COPY admin-frontend/nginx-site.conf /etc/nginx/conf.d/default.conf

RUN rm -f /usr/share/nginx/html/Dockerfile \
          /usr/share/nginx/html/server.py \
          /usr/share/nginx/html/nginx-site.conf \
 && rm -rf /usr/share/nginx/html/login-bg-options \
           /usr/share/nginx/html/__pycache__

EXPOSE 80

HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1/healthz || exit 1
