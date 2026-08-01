# FutuOpenD-rs (Rust gateway) image — amd64 only (upstream publishes x86_64).
# Build:
#   docker build --platform linux/amd64 -t futu-opend-rs:1.5.0 -f configs/futu-opend-rs.Dockerfile .
# Credentials: mount ~/.futu-opend-rs at /root/.futu-opend-rs (remember-login survives restarts).
FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends libdbus-1-3 \
    && rm -rf /var/lib/apt/lists/*

ARG FUTU_VERSION=1.5.0
ARG FUTU_SHA256=7e1411e0f1ee0c8f28339b67abe789fbdbf84b0b1b0293edded625ad22ff8ef9

ADD https://futuapi.com/releases/rs-v${FUTU_VERSION}/futu-opend-rs-${FUTU_VERSION}-linux-x86_64.tar.gz /tmp/futu.tar.gz
RUN echo "${FUTU_SHA256}  /tmp/futu.tar.gz" | sha256sum -c - \
    && tar xzf /tmp/futu.tar.gz -C /opt \
    && rm /tmp/futu.tar.gz

VOLUME /root/.futu-opend-rs
EXPOSE 11111 22222 33333

ENTRYPOINT ["/opt/futu-opend-rs-1.5.0/futu-opend"]
