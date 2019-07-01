# grafeas-pgsql

[Grafeas](https://github.com/grafeas/grafeas) with PostgreSQL backend as a Docker Compose, provides a standalone instance of the Grafeas server with PostgreSQL as the storage layer.

## Running Standalone Grafeas with PostgreSQL

To start the standalone instance of Grafeas with PostgreSQL, follow these steps:

1. Install [Docker Compose](https://docs.docker.com/compose/install/).
1. Pull the Grafeas Server image:

   ```bash
   docker pull us.gcr.io/grafeas/grafeas-server:0.1.0
   ```

1. Build the images and run the containers:

   ```bash
   docker-compose build && docker-compose up
   ```

1. Ensure you can reach the server:

```bash
curl http://localhost:8080/v1beta1/projects
```

## Support

Please refer to [Grafeas Support](https://github.com/grafeas/grafeas#support) page.

## Contributing

Pull requests and feature requests as GH issues are welcome!

## License
Grafeas is under the Apache 2.0 license. See the [LICENSE](LICENSE) file for details.
