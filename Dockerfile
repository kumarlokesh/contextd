# Multi-stage build for contextd
FROM rust:1.70 AS builder

# Install system dependencies
RUN apt-get update && apt-get install -y \
    sqlite3 \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

# Create app directory
WORKDIR /app

# Copy dependency files
COPY Cargo.toml Cargo.lock ./

# Create dummy main.rs to cache dependencies
RUN mkdir src && echo "fn main() {}" > src/main.rs
RUN cargo build --release && rm -rf src

# Copy source code
COPY src/ src/
COPY contextd.toml ./

# Build the application
RUN touch src/main.rs && cargo build --release

# Runtime stage
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    sqlite3 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -m -u 1000 contextd

# Create data directory
RUN mkdir -p /data && chown contextd:contextd /data

# Copy binary and config
COPY --from=builder /app/target/release/contextd /usr/local/bin/contextd
COPY --from=builder /app/target/release/contextctl /usr/local/bin/contextctl
COPY --from=builder /app/contextd.toml /etc/contextd/contextd.toml

# Update config for container environment
RUN sed -i 's|sqlite_path = "./data/contextd.db"|sqlite_path = "/data/contextd.db"|' /etc/contextd/contextd.toml && \
    sed -i 's|index_path = "./data/index"|index_path = "/data/index"|' /etc/contextd/contextd.toml && \
    sed -i 's|log_path = "./data/audit.log"|log_path = "/data/audit.log"|' /etc/contextd/contextd.toml && \
    sed -i 's|host = "127.0.0.1"|host = "0.0.0.0"|' /etc/contextd/contextd.toml

# Switch to non-root user
USER contextd

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD contextctl --url http://localhost:8080 health || exit 1

# Volume for persistent data
VOLUME ["/data"]

# Default command
CMD ["contextd", "serve", "--config", "/etc/contextd/contextd.toml"]
