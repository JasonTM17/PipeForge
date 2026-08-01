FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /opt/pipeforge/python

COPY python/pyproject.toml python/README.md ./
COPY python/src ./src
COPY contracts /opt/pipeforge/contracts

RUN pip install --no-cache-dir . \
    && useradd --system --create-home --home-dir /home/pipeforge pipeforge \
    && chown -R pipeforge:pipeforge /opt/pipeforge /home/pipeforge

USER pipeforge
EXPOSE 8090

HEALTHCHECK --interval=5s --timeout=3s --retries=12 CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8090/health/live', timeout=2)"

ENTRYPOINT ["python", "-m", "pipeforge_worker"]
