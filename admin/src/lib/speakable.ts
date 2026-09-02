/**
 * Turns chat text into something a TTS voice can say without embarrassing
 * itself. Mirrors the phone app's speakable.dart — same passes, same output,
 * so an agent sounds identical from either client.
 *
 * Pass one removes markdown STRUCTURE while keeping the words (a fenced block
 * becomes the words "code block" — nobody wants forty lines of bash read
 * aloud). Pass two verbalises numbers, because small TTS models are far
 * better at "one point zero point one" than at "1.0.1".
 */

export function speakable(text: string): string {
  return verbaliseNumbers(stripMarkdown(text));
}

// --------------------------------------------------------------- markdown ---

function stripMarkdown(text: string): string {
  let s = text.replace(/\r\n/g, "\n");

  // Fenced blocks first, before anything inside them can match other rules.
  s = s.replace(/```[^\n]*\n[\s\S]*?```/g, " code block. ");
  s = s.replace(/```[\s\S]*?```/g, " code block. ");

  // Images speak their alt text; links their label; bare URLs their host.
  s = s.replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1");
  s = s.replace(/\[([^\]]*)\]\([^)]*\)/g, "$1");
  s = s.replace(/https?:\/\/([^/\s)>\]]+)[^\s)>\]]*/g, "$1");

  // Inline code keeps its content: "run `make up`" should say "run make up".
  s = s.replace(/`([^`]*)`/g, "$1");

  const lines: string[] = [];
  for (let line of s.split("\n")) {
    const t = line.trim();
    // Horizontal rules and table separator rows carry no words.
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(t)) continue;
    if (/^\|?[\s:|-]+\|[\s:|-]*$/.test(t)) continue;
    line = line.replace(/^\s{0,3}#{1,6}\s+/, "");
    line = line.replace(/^\s{0,3}>\s?/, "");
    line = line.replace(/^\s*[-*+]\s+/, "");
    line = line.replace(/^\s*\d{1,3}[.)]\s+/, "");
    line = line.replace(/\|/g, " ");
    lines.push(line);
  }
  s = lines.join("\n");

  // Emphasis markers. Single underscores stay: stripping them would turn
  // snake_case identifiers into nonsense words.
  s = s.replace(/\*{1,3}/g, "");
  s = s.replace(/~~/g, "");
  s = s.replace(/__/g, "");

  // Anything shaped like an HTML tag.
  s = s.replace(/<\/?[a-zA-Z][^>]*>/g, " ");

  return s.replace(/[ \t]+/g, " ").replace(/\n{3,}/g, "\n\n").trim();
}

// ---------------------------------------------------------------- numbers ---

function verbaliseNumbers(s: string): string {
  // Money before anything eats the digits.
  s = s.replace(/\$(\d[\d,]*)(\.\d+)?/g, (_, whole: string, frac?: string) => {
    const w = intWords(whole.replace(/,/g, ""));
    if (!frac) return `${w} dollars`;
    return `${w} point ${digitWords(frac.slice(1))} dollars`;
  });

  // Percentages.
  s = s.replace(/(\d[\d,]*(?:\.\d+)?)\s?%/g, (_, n: string) => `${numberWords(n)} percent`);

  // Clock times: "3:30" → "three thirty", "3:00" → "three o'clock".
  s = s.replace(/\b(\d{1,2}):(\d{2})\b/g, (_, h: string, mm: string) => {
    const hw = intWords(h);
    if (mm === "00") return `${hw} o'clock`;
    if (mm.startsWith("0")) return `${hw} oh ${intWords(mm.slice(1))}`;
    return `${hw} ${intWords(mm)}`;
  });

  // Dotted versions ("1.0.1", "2.10.3") read group by group. Lookarounds, not
  // \b: "v1.0.1" has no word boundary between the v and the 1.
  s = s.replace(/(?<![\d.])(\d+(?:\.\d+){2,})(?![\d.])/g, (m: string) =>
    m.split(".").map(intWords).join(" point "),
  );

  // Ranges between small numbers: "steps 3-5" → "steps three to five".
  s = s.replace(/\b(\d{1,4})-(\d{1,4})\b/g, (_, a: string, b: string) =>
    `${intWords(a)} to ${intWords(b)}`,
  );

  // Decimals, then plain integers (comma thousands included).
  s = s.replace(/\b(\d[\d,]*)\.(\d+)\b/g, (_, i: string, f: string) =>
    `${intWords(i.replace(/,/g, ""))} point ${digitWords(f)}`,
  );
  s = s.replace(/(?<![\w.])(\d[\d,]*)(?![\w.%])/g, (m: string) =>
    intWords(m.replace(/,/g, "")),
  );

  return s.replace(/[ \t]+/g, " ").trim();
}

const ONES = [
  "zero", "one", "two", "three", "four", "five", "six", "seven", "eight",
  "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen",
  "sixteen", "seventeen", "eighteen", "nineteen",
];
const TENS = ["", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"];
const SCALES = ["", " thousand", " million", " billion", " trillion"];

/** English words for a non-negative integer given as digits; past the
 *  trillions it reads digit by digit. */
function intWords(digits: string): string {
  digits = digits.replace(/^0+(?=\d)/, "");
  if (digits.length > 15) return digitWords(digits);
  let n = Number(digits);
  if (n === 0) return "zero";

  const parts: string[] = [];
  let scale = 0;
  while (n > 0) {
    const chunk = n % 1000;
    if (chunk > 0) parts.unshift(`${chunkWords(chunk)}${SCALES[scale]}`);
    n = Math.floor(n / 1000);
    scale++;
  }
  return parts.join(" ");
}

function chunkWords(n: number): string {
  const out: string[] = [];
  if (n >= 100) {
    out.push(`${ONES[Math.floor(n / 100)]} hundred`);
    n %= 100;
  }
  if (n >= 20) {
    const t = TENS[Math.floor(n / 10)];
    n %= 10;
    out.push(n > 0 ? `${t}-${ONES[n]}` : t);
  } else if (n > 0) {
    out.push(ONES[n]);
  }
  return out.join(" ");
}

function digitWords(digits: string): string {
  return digits.split("").map((d) => ONES[Number(d)]).join(" ");
}

function numberWords(raw: string): string {
  raw = raw.replace(/,/g, "");
  const dot = raw.indexOf(".");
  if (dot < 0) return intWords(raw);
  return `${intWords(raw.slice(0, dot))} point ${digitWords(raw.slice(dot + 1))}`;
}
