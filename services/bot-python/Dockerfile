FROM python:3.12-alpine
WORKDIR /app
COPY bot.py ./bot.py
COPY requirements.txt ./requirements.txt
RUN pip install --no-cache-dir -r requirements.txt
RUN adduser -D -u 10001 app
USER 10001
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
CMD ["python", "bot.py"]
