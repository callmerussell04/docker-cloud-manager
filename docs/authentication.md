# Аутентификация

DCM поддерживает локальные учётные данные и вход через Keycloak/OIDC.

## Локальная аутентификация

Параметры начального администратора и доступные в браузере способы входа задаются в `backend/.env`:

```env
SSO_ADMIN_USERNAME=admin
SSO_ADMIN_EMAIL=admin@example.com
SSO_ADMIN_PASSWORD=<надёжный-случайный-пароль>
SSO_LOCAL_LOGIN_ENABLED=true
SSO_LOCAL_REGISTER_ENABLED=true
```

- `SSO_LOCAL_LOGIN_ENABLED` управляет входом по имени пользователя и паролю.
- `SSO_LOCAL_REGISTER_ENABLED` управляет самостоятельной регистрацией.
- Bootstrap admin остаётся резервной локальной учётной записью.
- Минимальная длина локального пароля на границе HTTP API — 6 символов; для администратора используйте более длинный случайный пароль.

## Модель токенов DCM

1. Вход по паролю или обратный вызов OIDC создаёт токен обновления DCM в файле cookie с атрибутом HttpOnly.
2. Клиентская часть получает короткоживущий токен доступа через `/api/v1/auth/refresh`.
3. Gateway проверяет токен доступа через SSO.
4. Выход из системы удаляет файл cookie с токеном обновления.

Для развёртывания по HTTP используются:

```env
COOKIE_SECURE=false
COOKIE_HTTP_ONLY=true
COOKIE_SAMESITE=lax
```

При появлении HTTPS необходимо изменить origins/URLs и установить `COOKIE_SECURE=true`.

## OIDC через Keycloak

В текущей версии Keycloak является единственным поставщиком OIDC, к которому DCM подключается напрямую. Других поставщиков удостоверений можно подключить через Keycloak в качестве посредника.

Основные переменные:

```env
SSO_OIDC_ENABLED=true
SSO_OIDC_ISSUER_URL=http://keycloak.localhost/realms/dcm
SSO_OIDC_CLIENT_ID=dcm
SSO_OIDC_CLIENT_SECRET=<секрет-клиента>
SSO_OIDC_REDIRECT_URL=http://localhost/api/v1/auth/oidc/keycloak/callback
SSO_OIDC_ADMIN_GROUPS=/dcm-admins,dcm-admin
SSO_OIDC_DEFAULT_ROLE=user
```

### Настройка Keycloak для разработки

1. Запустите дополнительный Compose-файл:

   ```bash
   docker compose --env-file backend/.env \
     -f docker-compose.yml \
     -f backend/docker-compose.keycloak.yml \
     up -d keycloak_db keycloak
   ```

2. Откройте `http://keycloak.localhost`.
3. Создайте realm `dcm`.
4. Создайте confidential OpenID Connect client `dcm`.
5. Включите client authentication и standard authorization code flow.
6. Добавьте valid redirect URI `http://localhost/api/v1/auth/oidc/keycloak/callback`.
7. Добавьте post logout redirect `http://localhost/*` и web origins `http://localhost`, `http://localhost:5173`.
8. Скопируйте client secret в `SSO_OIDC_CLIENT_SECRET` и перезапустите SSO/Gateway.


### Сопоставление ролей

DCM сопоставляет группы и роли области Keycloak со значениями из `SSO_OIDC_ADMIN_GROUPS`. Чтобы назначать роль администратора:

- создайте группу `/dcm-admins` или роль области `dcm-admin`;
- добавьте преобразователь, включающий `groups` и/или `realm_access.roles` в ID-токен;
- назначьте группу или роль нужным пользователям.

Пользователь OIDC создаётся при первом успешном входе. Внешний идентификатор пользователя остаётся постоянным; имя пользователя и адрес электронной почты могут синхронизироваться при последующих входах. При конфликте с локальным именем пользователя или адресом электронной почты автоматическое создание учётной записи отклоняется.

## Безопасность потока OIDC

Gateway устанавливает привязанный к браузеру файл cookie `oidc_state` с атрибутом HttpOnly. SSO проверяет состояние, одноразовое значение, издателя и код авторизации при обратном вызове. После него Gateway устанавливает файл cookie с токеном обновления DCM и перенаправляет одностраничное приложение на `/auth/callback`; токены DCM не появляются в строке запроса.

## Диагностика

- Если provider не отображается, проверьте `SSO_OIDC_ENABLED` и `/api/v1/auth/providers`.
- Если callback отклонён, сравните issuer, redirect URI и browser origin без отличий в scheme/host/port.
- Если admin role не назначается, проверьте token mappers и точное значение group/role.
- После изменения OIDC env перезапустите `sso` и `gateway`.

[К справочнику конфигурации](configuration.md) · [К оглавлению](README.md)
