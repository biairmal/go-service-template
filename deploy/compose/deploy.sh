#!/bin/sh
# Deploy a new version without dropping requests: pull, migrate, start new api
# replicas next to the old ones, wait until each new one is ready, then stop
# the old ones. Stopping drains: /readyz answers 503, Caddy stops sending work,
# in-flight requests finish. Set IMAGE_TAG in .env first.
#
# Usage, in deploy/compose on the VPS: ./deploy.sh
set -eu
cd "$(dirname "$0")"

replicas=${API_REPLICAS:-2}
ready_timeout=${READY_TIMEOUT:-120}

docker compose pull --quiet
docker compose run --rm migrate

old=$(docker compose ps -q api)
docker compose up -d --no-deps --no-recreate --scale "api=$((replicas * 2))" api
new=$(docker compose ps -q api | grep -vxF "$old" || true)
[ -n "$new" ] || { echo "deploy: no new api replica started" >&2; exit 1; }

for c in $new; do
	ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$c")
	waited=0
	# The probe runs from the Caddy container: the api image has no shell.
	until docker compose exec -T caddy wget -q -O /dev/null -T 2 "http://$ip:8080/readyz" 2>/dev/null; do
		waited=$((waited + 1))
		if [ "$waited" -ge "$ready_timeout" ]; then
			echo "deploy: replica $c not ready after ${ready_timeout}s; removing the new replicas, the old ones keep serving" >&2
			docker logs --tail 50 "$c" >&2 || true
			# shellcheck disable=SC2086 # one ID per word
			docker rm -f $new >/dev/null
			exit 1
		fi
		sleep 1
	done
	echo "deploy: replica $c ready ($ip)"
done

if [ -n "$old" ]; then
	# shellcheck disable=SC2086 # one ID per word
	docker stop $old >/dev/null
	# shellcheck disable=SC2086
	docker rm $old >/dev/null
fi

# Everything else (worker, Caddy, backup) follows the Compose file as usual.
docker compose up -d --wait
echo "deploy: done, $(docker compose ps -q api | wc -l | tr -d ' ') api replicas"
