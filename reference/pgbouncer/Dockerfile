# PgBouncer from Alpine's package repository (pgbouncer 1.24.x on Alpine 3.22).
FROM alpine:3.22
RUN apk add --no-cache pgbouncer \
 && addgroup -S pgb && adduser -S -G pgb pgb
# PgBouncer refuses to run as root.
USER pgb
EXPOSE 6432
CMD ["pgbouncer", "/etc/pgbouncer/pgbouncer.ini"]
