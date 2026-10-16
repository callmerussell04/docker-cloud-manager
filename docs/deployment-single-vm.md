# Развёртывание Docker Cloud Manager на одной машине

Инструкция описывает установку полного набора служб DCM через корневой `docker-compose.yml`. Все приложения, базы данных, очередь, объектное хранилище, реестр образов и Traefik запускаются на одной машине с Linux.

> [!WARNING]
> Текущий Compose принимает внешний трафик по HTTP на порту `80`. HTTPS/ACME не настроены.

## Ограничения текущего варианта

- Один Docker Engine является единственной средой выполнения контейнеров.
- Внешняя точка входа — HTTP `:80`.
- Registry публикуется только на `127.0.0.1:5000` и рассчитан на тот же сервер Docker.
- PostgreSQL, ClickHouse, RabbitMQ и MinIO запускаются на той же машине.
- Высокая доступность и автоматическое восстановление узла не предусмотрены.

## 1. Требования

Подготовьте:

- Linux-машину с публичным IPv4 или IPv6;
- Docker Engine и Docker Compose plugin;
- пользователя с доступом к Docker daemon;
- свободный входящий TCP-порт `80`;
- домен панели, например `dcm.example.com`;
- отдельный wildcard-домен приложений, например `*.apps.example.com`;
- `git`, `curl`, `openssl`, Python 3 и утилиту `dig` либо аналог для проверки DNS.

Проверьте окружение:

```bash
docker version
docker compose version
docker info
sudo ss -ltnp '( sport = :80 )'
```

Последняя команда не должна показывать процесс, занимающий `0.0.0.0:80` или `[::]:80`.

## 2. Настройте DNS

Создайте записи, ведущие на адрес сервера:

```text
dcm.example.com       A/AAAA  <server-ip>
*.apps.example.com    A/AAAA  <server-ip>
```

Проверьте обе записи:

```bash
dig +short dcm.example.com
dig +short test.apps.example.com
```

Оба имени должны возвращать адрес сервера. `DCM_HOST` будет равен `dcm.example.com`, а `BASE_DOMAIN` — `apps.example.com`.

## 3. Расширьте пул Docker-сетей

DCM создаёт отдельную bridge-сеть для каждого пользователя. Стандартного пула Docker может не хватить уже после нескольких десятков сетей.

Перед первым запуском добавьте свободный адресный диапазон в `/etc/docker/daemon.json`. Не используйте диапазон, пересекающийся с LAN, VPN, cloud VPC или существующими Docker networks.

```json
{
  "default-address-pools": [
    {
      "base": "10.10.0.0/16",
      "size": 24
    }
  ]
}
```

Если файл уже содержит настройки, добавьте ключ в существующий JSON-объект. Затем проверьте JSON и перезапустите Docker:

```bash
python3 -m json.tool /etc/docker/daemon.json >/dev/null
sudo systemctl restart docker
docker info
```

Перезапуск Docker останавливает работающие контейнеры; на используемом сервере предварительно согласуйте окно обслуживания.

## 4. Получите исходный код

```bash
git clone https://github.com/callmerussell04/docker-cloud-manager.git
cd docker-cloud-manager
```

Все следующие Compose-команды выполняются из корня репозитория.

## 5. Создайте env-файл

Скопируйте публичный шаблон:

```bash
cp backend/.env.example backend/.env
chmod 600 backend/.env
```

Сгенерируйте независимые значения для секретов:

```bash
openssl rand -hex 32
```

Выполните команду отдельно для каждого секрета и замените значения `change_me_*`. Как минимум задайте уникальные значения для:

- `POSTGRES_PASSWORD`;
- `JWT_SECRET`;
- `INTERNAL_SERVICE_TOKEN`;
- `SSO_ADMIN_PASSWORD`;
- `RABBITMQ_PASSWORD`;
- `MINIO_ROOT_PASSWORD` и совпадающего с ним `OBJECT_STORAGE_SECRET_KEY`;
- `CLICKHOUSE_PASSWORD`.

Переменные Keycloak обязательны только при запуске дополнительного Keycloak Compose-файла, но оставлять опубликованные значения `change_me_*` небезопасно.

## 6. Настройте домены и браузерный доступ

Для примера выше установите:

```env
SSO_ADMIN_EMAIL=admin@example.com
DCM_HOST=dcm.example.com
BASE_DOMAIN=apps.example.com
FRONTEND_API_URL=http://dcm.example.com/api/v1
CORS_ALLOWED_ORIGINS=http://dcm.example.com
GATEWAY_AUTH_REDIRECT_ALLOWED_ORIGINS=http://dcm.example.com
COOKIE_SECURE=false
COOKIE_SAMESITE=lax
```

`FRONTEND_API_URL` и `BASE_DOMAIN` встраиваются в клиентскую часть во время сборки. После изменения этих значений её необходимо пересобрать.

Для реестра образов на одной машине оставьте:

```env
REGISTRY_API_URL=https://registry-backend:5000
REGISTRY_URL=registry:5000
REGISTRY_PUBLIC_URL=registry:5000
```

OIDC по умолчанию выключен:

```env
SSO_OIDC_ENABLED=false
```

Полное назначение переменных описано в [справочнике конфигурации](configuration.md).

Перед первым запуском выполните раздел «TLS и изоляция Registry» ниже.

## 7. Проверьте конфигурацию

```bash
docker compose --env-file backend/.env config --quiet
```

Успешная команда ничего не выводит и завершается с кодом `0`. Ошибка вида `VARIABLE is required` означает, что обязательное значение отсутствует.

## 8. Запустите стек

```bash
docker compose --env-file backend/.env up -d --build
```

Compose последовательно запускает базы данных и инфраструктурные службы, применяет миграции PostgreSQL и ClickHouse, а затем запускает службы DCM. Первая сборка может занять несколько минут.

Проверьте состояние:

```bash
docker compose --env-file backend/.env ps --all
```

Ожидаемый результат:

- `core_db`, `sso_db`, `clickhouse`, `rabbitmq`, `minio` и `frontend` имеют состояние `healthy`;
- `gateway`, `sso`, `core`, `builder`, `telemetry`, `traefik` и `registry` находятся в состоянии `running`;
- `migrate_core`, `migrate_sso`, `migrate_reports` завершились с кодом `0`.

При ошибке перейдите к разделу [«Службы не запускаются или проверка готовности возвращает 503»](troubleshooting.md#службы-не-запускаются-или-проверка-готовности-возвращает-503).

## 9. Проверьте доступность

На сервере проверьте маршрутизацию с правильным HTTP-заголовком `Host`:

```bash
curl -fsS -H 'Host: dcm.example.com' http://127.0.0.1/health/live
curl -fsS -H 'Host: dcm.example.com' http://127.0.0.1/health/ready
```

С внешней машины:

```bash
curl -fsS http://dcm.example.com/health/live
curl -fsS http://dcm.example.com/health/ready
curl -I http://dcm.example.com/
```

Проверка жизнеспособности должна вернуть `status: ok`. Проверка готовности возвращает HTTP `200`, только когда Gateway видит Core по gRPC и HTTP, SSO по gRPC и Telemetry по HTTP.

## 10. Выполните первый вход

Откройте:

```text
http://dcm.example.com/
```

Используйте значения из `backend/.env`:

- имя пользователя — `SSO_ADMIN_USERNAME`;
- пароль — `SSO_ADMIN_PASSWORD`.

После входа:

1. Откройте системный мониторинг и убедитесь, что ресурсы сервера доступны.
2. Создайте тестового пользователя и назначьте квоты.
3. Выполните [минимальную проверку основных возможностей](user-guide.md#минимальная-проверка-основных-возможностей) из руководства пользователя.

## Остановка

Безопасная остановка с сохранением данных:

```bash
docker compose --env-file backend/.env down
```

> [!CAUTION]
> Не добавляйте `-v`, если не требуется безвозвратно удалить базы данных, object storage, registry и очереди.

## Следующие шаги

- [Руководство администратора](admin-guide.md)
- [Аутентификация и OIDC](authentication.md)
- [Диагностика](troubleshooting.md)

[К оглавлению документации](README.md)

## TLS и изоляция Registry

Перед запуском подготовьте `REGISTRY_CERTS_DIR` (например `/etc/dcm/registry`) с правами, ограничивающими чтение private keys:

- `ca.crt` — CA для TLS;
- `registry.crt`, `registry.key` — TLS certificate/key для Core edge, SAN включает `registry`;
- `backend.crt`, `backend.key` — TLS certificate/key backend, SAN включает `registry-backend`;
- `signing.crt`, `signing.key` — отдельный RSA signing certificate/key Core. Используйте отдельный self-signed certificate либо certificate, непосредственно доверенный в `signing.crt` Registry. Приватный signing key монтируется только в Core.

Создайте сертификаты своей CA, задайте `REGISTRY_HTTP_SECRET` независимо от остальных секретов. Включите `br_netfilter` и `net.bridge.bridge-nf-call-iptables=1`, `net.bridge.bridge-nf-call-ip6tables=1`. Compose требует iptables backend Docker и устанавливает ограничения для отдельных bridges `br-dcm-build`/`br-dcm-egress`; не используйте эти имена для других сетей. Диапазон `172.30.252.0/24` должен быть свободен.

Для single-server конфигурации установите `REGISTRY_URL=registry:5000` и `REGISTRY_PUBLIC_URL=registry:5000`. Добавьте на Docker host `registry` в `/etc/hosts` с адресом `127.0.0.1` и установите `ca.crt` в `/etc/docker/certs.d/registry:5000/ca.crt`. Docker должен доверять этой CA. Канонический TLS realm должен быть достижим по одному имени и из Kaniko, и из daemon. Для другого DNS имени обновите SAN, host DNS, оба адреса и network alias согласованно.

Порт `127.0.0.1:5000` принадлежит TCP forwarder `registry-edge`, который передаёт TLS только в аутентифицированный listener Core. Core не подключается к сети сборки. Сам Registry не публикует порты и находится в отдельной внутренней сети. Не добавляйте его в `build_net`, `dcm_net` или Traefik. Запрос без credentials к `https://registry:5000/v2/` должен вернуть `401` и Bearer challenge; plaintext HTTP и Core management paths на этом порту недоступны.

`build-network-policy` требует `CAP_NET_ADMIN` и host network для установки host firewall. `build-egress` обеспечивает доступ Dockerfile к внешним пакетам по HTTP(S) через `HTTP_PROXY`/`HTTPS_PROXY`; клиенты, игнорирующие proxy, не получают прямой интернет-доступ. Builder ждёт готовности policy. Для обновления существующего стенда остановите новые сборки, дождитесь завершения текущих, затем пересоздайте `build_net` как internal network. Согласуйте действующий host firewall с этими правилами; не снимайте ограничения при остановке worker.

Изолированная проверка (только disposable test database):

```bash
cd backend
CORE_TEST_REGISTRY_ISOLATION=1 CORE_TEST_DATABASE_URL='postgres://.../dcm_core_test?sslmode=disable' \
  GOCACHE=/tmp/go-build-cache go test -count=1 -timeout=5m ./tests/core/integration/registry_isolation
```

Тест создаёт собственные Registry storage, bridge и execution containers, проверяет реальные Dockerfile RUN с прочитанным job credential и удаляет свои fixtures. Он не устанавливает host firewall и не проверяет policy container; перед эксплуатацией отдельно проверьте сетевую политику из execution network.

Для изолированной проверки host firewall с уникальными временными bridges/chains и их удалением после теста:

```bash
cd backend
CORE_TEST_NETWORK_POLICY=1 GOCACHE=/tmp/go-build-cache \
  go test -count=1 -timeout=3m -run TestBuildNetworkPolicy ./tests/core/integration/registry_isolation
```
