import { describe, expect, it } from "vitest";
import { LONG_PRESS_MS, edgeSpeed, pressIntent } from "./pointerDrag";

describe("pressIntent", () => {
  it("a mouse or pen drags once it moves past the slop, and a click stays a click", () => {
    expect(pressIntent("mouse", 2, 5000)).toBe("wait");
    expect(pressIntent("mouse", 6, 10)).toBe("drag");
    expect(pressIntent("pen", 6, 10)).toBe("drag");
  });

  it("a finger drags only after holding still, and moving first gives the gesture to scrolling", () => {
    expect(pressIntent("touch", 3, 100)).toBe("wait");
    expect(pressIntent("touch", 3, LONG_PRESS_MS)).toBe("drag");
    expect(pressIntent("touch", 14, 100)).toBe("release");
    expect(pressIntent("touch", 14, LONG_PRESS_MS + 100)).toBe("release");
  });
});

describe("edgeSpeed", () => {
  it("is zero away from the edge and fastest at it", () => {
    expect(edgeSpeed(200)).toBe(0);
    expect(edgeSpeed(56)).toBe(0);
    expect(edgeSpeed(0)).toBe(14);
    expect(edgeSpeed(-20)).toBe(14);
    expect(edgeSpeed(28)).toBeGreaterThan(0);
    expect(edgeSpeed(28)).toBeLessThan(edgeSpeed(10));
  });
});
