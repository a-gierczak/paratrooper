# Paratrooper

Paratrooper is an over-the-air (OTA) update server for React Native applications. It serves as a drop-in replacement for EAS Update and CodePush, providing a self-hosted solution for managing and distributing app updates.

## Features

- **Drop-in Replacement**: Works seamlessly as a replacement for EAS Update and CodePush services
- **Multi-Protocol Support**: Supports both EAS Update and CodePush protocols
- **Cloud Provider Support**: Supports major cloud providers for file hosting, including:
  - AWS S3
  - Google Cloud Storage
- **Easy Integration**: Uses standard `expo-updates` and `react-native-code-push` clients - no need to install any additional dependencies
- **Self-Hosted**: Full control over your infrastructure

## Installation

### Prerequisites

- Docker and Docker Compose (for Docker setup)
- Go 1.22+ (for local development)
- PostgreSQL 13+
- NATS Server
- Redis (optional, for caching)

### Configuration

Before running Paratrooper, copy the example environment file:

```bash
cp .env.example .env
```

Edit `.env` to configure your database connection, storage provider, and other settings as needed.

## Running

### Docker (Recommended)

The easiest way to run Paratrooper is using Docker Compose:

```bash
docker compose up -d
```

This will start:
- The Paratrooper API server (port 8080)
- The Paratrooper update queue worker
- PostgreSQL database
- NATS message queue
- Redis cache (if configured)

### Local Development

To run Paratrooper locally outside of Docker:

1. Start the infrastructure services (PostgreSQL, NATS, Redis):

```bash
docker compose -f docker-compose.local.yml up -d
```

2. Run the API server:

```bash
make run-server
```

3. In a separate terminal, run the worker:

```bash
make run-worker
```

The API server will be available at `http://localhost:8080`.

When running locally, the database will be automatically seeded with two test projects that you can use for testing in your app:
- **CodePush Test** (ID: 0193a0f7-ba7d-742a-a9f6-3a14263f41f0)
- **Expo Test**: (ID: 019393ed-5085-71ec-943a-1c71617a6282)

## File Storage Configuration

Paratrooper supports two storage backends: local file storage or cloud storage via the [gocloud.dev/blob](https://gocloud.dev/howto/blob/) package. You must configure one of these options.

### Local File Storage

For local file storage, configure the following environment variables:

- `STORAGE_LOCAL_PATH` (default: `assets`) - The local directory path where files will be stored
- `STORAGE_LOCAL_SECRET_KEY_PATH` (required) - Path to a secret key file used for signing URLs. If the file doesn't exist, it will be automatically generated
- `API_PUBLIC_URL` (required) - The public URL of your Paratrooper API server (e.g., `http://localhost:8080` or `https://api.example.com`)

Example configuration:

```bash
STORAGE_LOCAL_PATH=/assets
STORAGE_LOCAL_SECRET_KEY_PATH=/app/.storage-secret-key
API_PUBLIC_URL=http://localhost:8080
```

### Cloud Storage

For cloud storage, use the `STORAGE_DRIVER_URL` environment variable with a driver URL in the gocloud.dev/blob format. Paratrooper supports:

- **AWS S3**: `s3://bucket-name?region=us-east-1`
- **Google Cloud Storage**: `gs://bucket-name`

The driver URL format follows the [gocloud.dev blob package](https://gocloud.dev/howto/blob/) conventions. You may need to configure additional environment variables or credentials depending on your cloud provider:

**AWS S3:**
- Set AWS credentials via environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`) or IAM roles
- Example: `STORAGE_DRIVER_URL=s3://my-bucket?region=us-east-1`

**Google Cloud Storage:**
- Set credentials via `GOOGLE_APPLICATION_CREDENTIALS` environment variable pointing to a service account JSON file
- Example: `STORAGE_DRIVER_URL=gs://my-bucket`

**Note:** Local storage and cloud storage are mutually exclusive. If `STORAGE_DRIVER_URL` is set, it will use cloud storage. Otherwise, configure local storage with `STORAGE_LOCAL_PATH`.

## Setting Up Your App

<!-- TODO: Add guide for setting up existing React Native app projects to work with Paratrooper -->

## License

See [LICENSE](LICENSE) file for details.
