FROM registry.codeocean.com/codeocean/ubuntu:22.04.1

ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        python3-pip=22.0.2+dfsg-1ubuntu0.7 \
        python3-setuptools=59.6.0-1.2ubuntu0.22.04.3 \
        python3-wheel=0.37.1-2ubuntu0.22.04.1 \
    && rm -rf /var/lib/apt/lists/*

COPY postInstall /tmp/tgovern-postInstall
RUN bash /tmp/tgovern-postInstall system
RUN bash /tmp/tgovern-postInstall python
COPY prepare-go-cache.sh experiment_config.py /tmp/tgovern-build-inputs/
COPY manifests /tmp/tgovern-build-inputs/manifests/
RUN bash /tmp/tgovern-postInstall cache
ENV PATH="/opt/tgovern/venv/bin:/opt/tgovern/go/bin:${PATH}" \
    SGX_SDK=/opt/intel/sgxsdk \
    SGX_MODE=SIM \
    GOTOOLCHAIN=local
WORKDIR /code
CMD ["bash", "run"]
