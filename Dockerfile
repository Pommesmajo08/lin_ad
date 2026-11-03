FROM scratch
COPY --chown=65532:65532 dist/linad-server /linad-server
# COPY creates a writable data directory for the unprivileged process.
COPY --chown=65532:65532 docker-data/ /data/
USER 65532:65532
ENV LINAD_LISTEN=0.0.0.0:8080 LINAD_DATA=/data/directory.json
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/linad-server"]
