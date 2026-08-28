import { useEffect, useRef, useState } from "react";
import { getToken } from "./api";

export interface FleetEvent {
  type: string;
  instance_id?: string;
  task_id?: string;
  payload?: unknown;
  at: string;
}

/**
 * Live event feed with reconnect.
 *
 * The socket is the difference between a console you trust and one you refresh:
 * instance state, agent steps, stats and alerts all arrive here rather than
 * being polled.
 */
export function useEvents(instanceId?: string, onEvent?: (e: FleetEvent) => void) {
  const [connected, setConnected] = useState(false);
  const handler = useRef(onEvent);
  handler.current = onEvent;

  // The token is a dependency, not just a value read inside the effect: the
  // socket has to come up the moment someone signs in, not on the next reload.
  const token = getToken();

  useEffect(() => {
    let socket: WebSocket | null = null;
    let retry: number | undefined;
    let attempt = 0;
    let closed = false;

    const connect = () => {
      if (!token) return;
      const scheme = location.protocol === "https:" ? "wss" : "ws";
      const params = new URLSearchParams({ token });
      if (instanceId) params.set("instance_id", instanceId);
      socket = new WebSocket(`${scheme}://${location.host}/api/events?${params}`);

      socket.onopen = () => {
        attempt = 0;
        setConnected(true);
      };
      socket.onmessage = (msg) => {
        try {
          handler.current?.(JSON.parse(msg.data) as FleetEvent);
        } catch {
          /* ignore malformed frames */
        }
      };
      socket.onclose = () => {
        setConnected(false);
        if (closed) return;
        // Exponential backoff, capped: a restarting orchestrator should not get
        // hammered by every open browser tab.
        attempt += 1;
        retry = window.setTimeout(connect, Math.min(1000 * 2 ** attempt, 15000));
      };
      socket.onerror = () => socket?.close();
    };

    connect();
    return () => {
      closed = true;
      if (retry) window.clearTimeout(retry);
      socket?.close();
    };
  }, [instanceId, token]);

  return connected;
}
