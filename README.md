# Galvanize Instancer

Galvanize Instancer is a lightweight service that deploys on-demand CTF challenge instances via Ansible and Docker. It is designed to work alongside the Zync CTFd plugin (https://github.com/28Pollux28/zync) and provides a simple HTTP API for deploy, status, extend, and terminate workflows.

## Highlights

- Per-team challenge instances with TTL and extension controls
- Ansible playbooks for HTTP, TCP, or custom Compose deployments
- Simple YAML challenge definitions matching CTFd challenge format
- JWT-protected API for easy integration with CTF platforms

## How It Works

- The Instancer service runs in a container and exposes an HTTP API.
- Challenges are defined in `data/challenges/*/challenge.yml`.
- Ansible playbooks in `data/playbooks/` handle deployments on your target hosts.
- Target hosts must be reachable over SSH and have Docker installed.
- HTTP challenges use Traefik to automatically set the domain name and SSL.

## Quick Start (Docker Compose)

1. Create your configuration file:

    ```bash
    cp config.example.yaml config.yaml
    ```

2. Edit `config.yaml` to set:

   - `auth.jwt_secret`
   - `instancer.instancer_host`
   - `instancer.ansible.inventory`
   - `instancer.ansible.user`
   - `instancer.ansible.private_key`

3. Update the SSH key mount in `docker-compose.yml`:

    ```yaml
        volumes:
          - /path/to/your/ssh/key:/home/galvanize/.ssh/ansible-ssh:Z,ro
    ```

4. Start the service:

    ```bash
    docker compose up -d --build
    ```

5. Check health:

    ```bash
    curl -f http://localhost:8080/health
    ```

### Optional: Monitoring (Prometheus + Grafana)

Append the monitoring overlay to bring up Prometheus and Grafana alongside the instancer:

```bash
docker compose -f docker-compose.yml -f docker-compose.monitoring.yml up -d --build
```

| Service    | URL                    | Credentials  |
|------------|------------------------|--------------|
| Grafana    | http://localhost:3000  | admin / admin |
| Prometheus | http://localhost:9090  | —            |

Grafana is pre-provisioned with the **Galvanize CTF Instancer** dashboard covering deployment counts, operation durations, extension stats, worker queue metrics, HTTP API latency, and more. The instancer exposes Prometheus metrics on port **5001** (`/metrics`).

## Zync CTFd Plugin Integration

Configure the Zync plugin to use your Instancer base URL and the same JWT secret you set in `config.yaml`.

- Base URL example: `http://your-instancer-host:8080`
- JWT secret: `auth.jwt_secret` in `config.yaml`

Refer to the Zync plugin documentation for the exact configuration fields.

## API Overview

The API is documented in `galvanize-instancer/api/openapi.yaml`. Main endpoints:

- `POST /deploy`
- `GET /status`
- `POST /extend`
- `POST /terminate`
- `GET /health`

All endpoints except `/health` require a JWT bearer token. Tokens are generated automatically by the Zync plugin.

## Challenge Definitions

Example challenge files (one per playbook type):

- `data/challenges/example/http/challenge.yml`
- `data/challenges/example/tcp/challenge.yml`
- `data/challenges/example/custom_compose/challenge.yml` (with a standalone `docker-compose.yml`)

Key fields:

- `name`, `author`, `category`
- `playbook_name`: `http`, `tcp`, or `custom_compose`
- `deploy_parameters`: image, ports, env, or Compose definition
- `flags`, `value`, `description`, `tags`

### Multi-service / Docker Compose challenges

For challenges that need more than one container (databases, sidecars, custom
networks/volumes), drop a standard Docker Compose file next to your
`challenge.yml`:

```
data/challenges/web/my-chall/
├── challenge.yml
└── docker-compose.yml
```

Galvanize auto-detects the first of `compose.yaml`, `compose.yml`,
`docker-compose.yaml`, or `docker-compose.yml` in the challenge directory,
validates it, and deploys it. When a compose file is present, `playbook_name`
defaults to `custom_compose`, so a minimal `challenge.yml` is enough:

```yaml
name: my-chall
author: YourName
category: web
type: zync

deploy_parameters:
  unique: false
```

This lets you develop and test the stack locally with a plain
`docker compose up`, then ship the file unchanged.

#### Exposing services (`expose`)

Compose challenges no longer need hand-written Traefik labels, manual network
wiring, or hand-picked host ports. Declare an `expose` block and Galvanize wires
the networking for you — the same automation the `http`/`tcp` playbooks provide
for single containers:

```yaml
deploy_parameters:
  unique: false
  expose:
    - service: web      # HTTP via Traefik → auto domain + SSL
      port: 80
      type: http
    - service: ssh      # raw TCP → published host port
      port: 22
      type: tcp
      scheme: ssh       # optional, only affects the rendered connection URL
```

For each entry Galvanize will, on deploy:

- `type: http` — attach the service to the external Traefik network, add the
  `traefik.enable` + router/service labels, and route
  `https://<project>.<instancer_host>/` to it (when more than one HTTP service
  is exposed, each gets a `<service>-<project>.<instancer_host>` subdomain).
  Requires `traefik_network` in `instancer.extra_deployment_parameters`.
- `type: tcp` — publish the container port. With
  `instancer.randomize_published_ports` enabled, the host port is randomized per
  team and persisted, so instances don't collide.

Connection info is returned automatically from the resulting Traefik route or
published port.

Notes:

- Use `deploy_parameters.compose_file: <name>` to point at a differently named
  file (relative to the challenge directory).
- An inline `deploy_parameters.compose_definition` string still works and takes
  precedence over any compose file, so existing challenges are unaffected.
- `expose` is optional: if you prefer, you can still hand-write Traefik labels
  and `ports:` in the compose file and omit `expose` entirely.
- The project name is generated automatically per team; do not set
  `container_name` in services or instances will collide.
- Image `build:` contexts and host bind mounts reference paths on the **target
  deploy host**, not the instancer — prefer pre-built images.

## Troubleshooting

- If deployments fail, verify SSH access to the target host and that Docker is installed.
- Ensure the mounted SSH key path in `docker-compose.yml` matches `instancer.ansible.private_key`.
- Check container logs with `docker logs galvanize-instancer`.
