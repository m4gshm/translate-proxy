FROM scratch
COPY translate-proxy /
COPY config.yaml /.config/translate-proxy/config.yaml
CMD ["/translate-proxy", "--address", "0.0.0.0:8080", "--insecure" , "--accesslog"]

EXPOSE 8080