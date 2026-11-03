FROM debian:bookworm-slim
RUN --mount=type=secret,id=proxy_ca \
    set -eu; set --; \
    if [ -f /run/secrets/proxy_ca ]; then set -- -o Acquire::https::CaInfo=/run/secrets/proxy_ca; fi; \
    apt-get "$@" update; \
    DEBIAN_FRONTEND=noninteractive apt-get "$@" install -y --no-install-recommends libpam-runtime libpam-modules pamtester openssl python3 ca-certificates passwd; \
    rm -rf /var/lib/apt/lists/*
