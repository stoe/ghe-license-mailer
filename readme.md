# ghe-license-mailer

> GitHub Enterprise Server license usage mailer

![Build](https://github.com/stoe/ghe-license-mailer/workflows/Go/badge.svg)

## Usage

```sh
USAGE:
       ghe-license-mailer [OPTIONS]

OPTIONS:
      --config string     path to the config file (defaults to $HOME/.ghe-license-mailer)
      --help              print this help
  -h, --hostname string   hostname
      --password string   admin password
  -p, --port int          admin port (default 8443)
      --to strings        email recipient(s), can be called multiple times
  -t, --token string      personal access token

EXAMPLE:

  $ ghe-license-mailer -h github.example.com -t AA123... --password P4s5...
```

If used with the default config file at `$HOME/.ghe-license-mailer`:

```sh
$ ghe-license-mailer
```

If used with a custom config file path:

```sh
$ ghe-license-mailer --config /path/to/.ghe-license-mailer
```

The script requires a personal access token with the `site_admin` scope. Create a Personal Access Token (PAT) for GitHub Enterprise Server, `https://HOSTNAME/settings/tokens/new?description=ghe-license-mailer&scopes=site_admin`

### Config file example

Filename: `.ghe-license-mailer`

```yml
---
hostname: github.example.com
token: PAT
password: ADMIN_PASSWORD
port: 8443

to:
  - example@github.com
  - admin@example.com
```

## License

MIT © [Stefan Stölzle](https://github.com/stoe)
