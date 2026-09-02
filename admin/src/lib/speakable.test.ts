import { describe, expect, it } from "vitest";
import { speakable } from "./speakable";

// The same vectors as the phone app's speakable_test.dart, on purpose: the
// two clients feed the same voice models, and an agent that sounds different
// per device is a bug with two homes.
describe("markdown structure", () => {
  it("emphasis markers vanish, words stay", () => {
    expect(speakable("**Done** — the *staging* run passed")).toBe(
      "Done — the staging run passed",
    );
  });

  it("a fenced block becomes the words 'code block'", () => {
    const out = speakable("Run this:\n```bash\nmake up && ./deploy.sh\n```\nthen check.");
    expect(out).toContain("code block");
    expect(out).not.toContain("make up");
    expect(out).not.toContain("```");
  });

  it("inline code keeps its content", () => {
    expect(speakable("run `make up` first")).toBe("run make up first");
  });

  it("links speak their label, bare URLs their host", () => {
    expect(speakable("see [the docs](https://example.com/docs/x)")).toBe("see the docs");
    expect(speakable("open https://example.com/deep/path?q=1 now")).toBe("open example.com now");
  });

  it("headings, quotes, bullets and tables lose their furniture", () => {
    const out = speakable(
      "## Result\n> quoted\n- first\n- second\n1. third\n\n| a | b |\n|---|---|\n| c | d |",
    );
    expect(out).not.toContain("#");
    expect(out).not.toContain(">");
    expect(out).not.toContain("|");
    expect(out).toContain("Result");
    expect(out).toContain("first");
    expect(out).toContain("c");
  });

  it("snake_case survives", () => {
    expect(speakable("set shell_access to on")).toBe("set shell_access to on");
  });
});

describe("numbers", () => {
  it("integers become words", () => {
    expect(speakable("ran 3 tasks across 21 steps")).toBe(
      "ran three tasks across twenty-one steps",
    );
    expect(speakable("1,250 records")).toBe("one thousand two hundred fifty records");
  });

  it("decimals read point-by-digit", () => {
    expect(speakable("took 3.14 seconds")).toBe("took three point one four seconds");
  });

  it("versions read group by group", () => {
    expect(speakable("now on v1.0.1")).toBe("now on vone point zero point one");
    expect(speakable("Airflow 2.10.3")).toBe("Airflow two point ten point three");
  });

  it("money and percent", () => {
    expect(speakable("costs $5 or 50% off")).toBe("costs five dollars or fifty percent off");
    expect(speakable("spent $1.25")).toBe("spent one point two five dollars");
  });

  it("clock times", () => {
    expect(speakable("at 3:30 then 4:00 then 9:05")).toBe(
      "at three thirty then four o'clock then nine oh five",
    );
  });

  it("ranges", () => {
    expect(speakable("steps 3-5 failed")).toBe("steps three to five failed");
  });

  it("huge numbers fall back to digits", () => {
    expect(speakable("id 1234567890123456")).toBe(
      "id one two three four five six seven eight nine zero one two three four five six",
    );
  });
});

it("the kitchen sink", () => {
  expect(
    speakable(
      "**Fixed!** Deployed `v2.1.0` — see [the run](https://ci.example.com/r/9). 3 tests, $0.02.",
    ),
  ).toBe(
    "Fixed! Deployed vtwo point one point zero — see the run. three tests, zero point zero two dollars.",
  );
});
