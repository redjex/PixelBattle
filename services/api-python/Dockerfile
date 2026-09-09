FROM python:3.12-slim
WORKDIR /app
COPY pyproject.toml ./
RUN pip install --no-cache-dir .
COPY app ./app
RUN useradd --system --uid 10001 app
USER 10001
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
EXPOSE 8000
CMD ["sh", "-c", "exec uvicorn app.main:app --host 0.0.0.0 --port 8000 --workers ${API_WORKERS:-4} --limit-concurrency ${API_LIMIT_CONCURRENCY:-200} --timeout-keep-alive 5 --no-access-log --no-proxy-headers"]
