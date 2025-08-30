# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

New API is a next-generation AI gateway and asset management system written in Go with a React frontend. It's a fork of One API that provides a unified interface for multiple AI service providers (OpenAI, Claude, Gemini, etc.) with advanced management features, billing, and multi-tenant support.

## Development Commands

### Backend Development
```bash
# Run backend development server
go run main.go

# Build and run with Makefile
make all                    # Build frontend and start backend
make build-frontend        # Build React frontend only
make start-backend         # Start Go backend only
```

### Frontend Development
```bash
# Navigate to web directory first
cd web

# Install dependencies
bun install

# Start development server
bun run dev

# Build for production
bun run build

# Lint and format
bun run lint               # Check code formatting
bun run lint:fix          # Auto-fix formatting issues
```

### Database Operations
```bash
# The application automatically handles database migrations on startup
# No manual migration commands needed - GORM auto-migrate is used

# To reset database (development only)
rm -rf data/new-api.db     # For SQLite
```

### Docker Development
```bash
# Start full stack (MySQL + Redis + New API)
docker-compose up -d

# Start with logs
docker-compose up

# Rebuild and restart
docker-compose up -d --build

# View logs
docker-compose logs new-api
```

## Architecture Overview

### Backend Structure (Go)
- **`main.go`**: Application entry point with server initialization
- **`controller/`**: HTTP handlers organized by feature (users, channels, billing, etc.)
- **`model/`**: GORM data models and database operations
- **`service/`**: Business logic services and utilities  
- **`middleware/`**: HTTP middleware (auth, rate limiting, CORS, etc.)
- **`router/`**: API route definitions organized by endpoint groups
- **`relay/`**: AI service adapters - the core gateway functionality
- **`common/`**: Shared utilities, constants, and helper functions
- **`dto/`**: Data transfer objects for API requests/responses

### Frontend Structure (React + Vite)
- **`web/src/`**: React application source
- **Styling**: Semi Design + Tailwind CSS
- **State Management**: React hooks + context
- **Build Tool**: Vite with TypeScript support
- **UI Library**: Semi Design by ByteDance

### Key Architectural Patterns
1. **Gateway Pattern**: Unified interface for multiple AI providers via channel adapters
2. **Middleware Chain**: Extensive middleware system for auth, rate limiting, logging
3. **Repository Pattern**: Models handle both data structure and database operations
4. **Adapter Pattern**: Each AI provider has its own standardized adapter

### Database Support
- **SQLite**: Default for development (file-based at `data/new-api.db`)
- **MySQL**: Recommended for production (≥5.7.8)
- **PostgreSQL**: Supported (≥9.6)
- **Logging Database**: Optional separate database for logs via `LOG_SQL_DSN`

### Caching Strategy
- **Redis**: Primary caching backend (configured via `REDIS_CONN_STRING`)
- **Memory Cache**: Fallback in-memory caching when Redis unavailable
- **Channel Cache**: Background sync of channel configurations for performance

## Environment Configuration

### Essential Environment Variables
```bash
# Database (choose one)
SQL_DSN="root:password@tcp(localhost:3306)/new-api"  # MySQL
# SQL_DSN="postgres://user:pass@localhost/new-api"   # PostgreSQL
# (SQLite is default if no SQL_DSN provided)

# Redis (recommended for production)
REDIS_CONN_STRING="redis://localhost:6379"

# Multi-node deployment
SESSION_SECRET="your-secret-key"      # Required for multi-node
CRYPTO_SECRET="your-crypto-key"       # Required if sharing Redis

# Security
GENERATE_DEFAULT_TOKEN=false          # Don't auto-create tokens for new users

# Performance
STREAMING_TIMEOUT=300                 # Stream timeout in seconds
MEMORY_CACHE_ENABLED=true            # Enable memory caching
```

### Development vs Production
- **Development**: Uses SQLite by default, GIN_MODE=debug
- **Production**: Requires MySQL/PostgreSQL, Redis, proper secrets set

## Testing and Quality

### Code Quality
- **Backend**: Uses Go standards, GORM for ORM, Gin for HTTP framework
- **Frontend**: Prettier for formatting, ESLint disabled in build
- **No formal test suite**: This is a limitation - tests need to be added manually

### Debugging
```bash
# Enable debug mode
export GIN_MODE=debug

# Enable profiling
export ENABLE_PPROF=true

# Enable error logging
export ERROR_LOG_ENABLED=true
```

## AI Provider Integration

### Channel System
The core feature is the channel adapter system in `relay/channel/`:
- Each AI provider has its own adapter (OpenAI, Claude, Gemini, etc.)
- Adapters normalize different APIs to a common interface
- Channel configuration includes API keys, base URLs, model mappings
- Automatic health checking and failover between channels

### Adding New Providers
1. Create adapter in `relay/channel/[provider]/`
2. Implement the standard interface methods
3. Add provider constants in `constant/channel.go`
4. Update frontend channel management UI

## Key Features Understanding

### Multi-Tenancy
- **Users**: Individual accounts with quotas and tokens
- **Groups**: User groups for access control
- **Token Groups**: API token organization and restrictions

### Billing System
- **Credit-based**: Users have quota balances
- **Per-model pricing**: Different costs for different AI models
- **Usage tracking**: Detailed logs of API calls and costs

### Rate Limiting
- **Global limits**: System-wide rate limiting
- **Per-user limits**: Individual user rate limits
- **Per-model limits**: Model-specific rate limiting

## Common Development Tasks

### Adding New API Endpoints
1. Add route in appropriate `router/` file
2. Create handler in `controller/`
3. Add DTOs in `dto/` if needed
4. Update frontend API calls in `web/src/`

### Database Schema Changes
1. Modify model structs in `model/`
2. GORM auto-migrate handles schema updates on startup
3. Consider data migration for breaking changes

### Environment-Specific Behavior
- Check `common/env.go` for environment variable handling
- Use `common.IsMasterNode` for distributed deployment features
- Database initialization in `model/database.go`