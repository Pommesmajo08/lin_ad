ARG BASE_IMAGE=debian:bookworm-slim
FROM ${BASE_IMAGE}
ARG SUDO_PACKAGE=sudo
RUN --mount=type=secret,id=proxy_ca \
    set -eu; set --; \
    if [ -f /run/secrets/proxy_ca ]; then set -- -o Acquire::https::CaInfo=/run/secrets/proxy_ca; fi; \
    apt-get "$@" update; \
    DEBIAN_FRONTEND=noninteractive apt-get "$@" install -y --no-install-recommends libpam-runtime libpam-modules pamtester openssl python3 ca-certificates passwd ${SUDO_PACKAGE}; \
    rm -rf /var/lib/apt/lists/*
# sudo-common 1.2ubuntu in the minimal Ubuntu image ships this as 0644;
# sudo-rs requires the standard secure mode before any integration can run.
RUN chmod 0440 /etc/sudoers
