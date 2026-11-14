ARG BASE_IMAGE=ubuntu:22.04
FROM ${BASE_IMAGE}
COPY environment/postInstall /tmp/tgovern-postInstall
RUN bash /tmp/tgovern-postInstall system
RUN bash /tmp/tgovern-postInstall python
ENV PATH="/opt/tgovern/venv/bin:/opt/tgovern/go/bin:${PATH}" \
    SGX_SDK=/opt/intel/sgxsdk \
    SGX_MODE=SIM \
    GOTOOLCHAIN=local
WORKDIR /code
CMD ["bash", "run"]
