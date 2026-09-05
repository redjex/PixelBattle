FROM python:3.12-alpine
WORKDIR /app
COPY bot.py ./bot.py
COPY requirements.txt ./requirements.txt
RUN pip install --no-cache-dir -r requirements.txt
CMD ["python", "bot.py"]
