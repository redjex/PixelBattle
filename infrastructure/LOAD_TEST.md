# Нагрузочная проверка

10 000 RPS нельзя подтвердить на машине разработчика или одним контейнером: итог зависит от CPU, сети, Redis, PostgreSQL и балансировщика. Production-схема должна запускать несколько экземпляров realtime и API за L7/L4 балансировщиком.

Перед тестом подними стек:

```powershell
docker compose -f infrastructure/compose.yaml up --build -d
```

Для HTTP smoke/load test можно использовать `bombardier`:

```powershell
bombardier -c 256 -d 30s -l http://localhost:8080/health
bombardier -c 256 -d 30s -l http://localhost:8000/health
```

Критерий приемки: 0 ошибок, p99 health-запроса ниже 100 ms на целевой инфраструктуре. Тестировать 10 000 RPS нужно с отдельной машины, постепенно повышая нагрузку и контролируя CPU, память, Redis stream lag, PostgreSQL connections и количество активных WebSocket-клиентов.
