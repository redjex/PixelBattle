# Нагрузочная проверка

10 000 RPS нельзя подтвердить на машине разработчика: итог зависит от CPU, сети, Redis, PostgreSQL и балансировщика. Realtime поддерживает только один экземпляр на доску; несколько экземпляров запрещены блокировкой PostgreSQL. Масштабирование требует отдельной распределённой архитектуры. Учитывайте ограничения запросов и настройте доверенный ingress по `docs/SECURITY.md`.

Перед тестом подними стек:

```powershell
docker compose --env-file .env -f infrastructure/compose.yaml up --build -d
```

Для HTTP smoke/load test можно использовать `bombardier`:

```powershell
bombardier -c 256 -d 30s -l http://localhost:8080/health
bombardier -c 256 -d 30s -l http://localhost:8000/health
```

Критерий приемки: 0 ошибок, p99 health-запроса ниже 100 ms на целевой инфраструктуре. Тестировать 10 000 RPS нужно с отдельной машины, постепенно повышая нагрузку и контролируя CPU, память, Redis stream lag, PostgreSQL connections и количество активных WebSocket-клиентов.
