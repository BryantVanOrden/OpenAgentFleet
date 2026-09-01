"""OpenAgentFleet exceptions."""

class FleetError(Exception):
    """Base exception for all OpenAgentFleet errors."""


class FleetApiError(FleetError):
    """Raised when the OpenAgentFleet HTTP API returns an error."""

    def __init__(self, message: str, status_code: int = 500) -> None:
        super().__init__(f"[{status_code}] {message}")
        self.message = message
        self.status_code = status_code


class FleetAuthError(FleetApiError):
    """Raised when authentication fails or token is expired."""


class FleetNotFoundError(FleetApiError):
    """Raised when an instance, task, or swarm is not found."""


class FleetTimeoutError(FleetError):
    """Raised when waiting for a task or instance times out."""
