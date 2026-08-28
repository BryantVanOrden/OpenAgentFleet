"""Deep Web Search & Research Engine for agentd.

Provides rapid real-time web intelligence and clean text synthesis without
requiring manual browser clicks.
"""

from __future__ import annotations

import re
import urllib.parse
import urllib.request
from typing import Any


USER_AGENT = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"


def _clean_html_to_markdown(html: str) -> str:
    """Strip scripts/styles and convert basic HTML to readable markdown text."""
    # Remove scripts, styles, head
    cleaned = re.sub(r"<(script|style|head|svg|noscript)[^>]*>.*?</\1>", "", html, flags=re.DOTALL | re.IGNORECASE)
    # Convert headings
    cleaned = re.sub(r"<h[1-3][^>]*>(.*?)</h[1-3]>", r"\n### \1\n", cleaned, flags=re.IGNORECASE | re.DOTALL)
    # Convert list items
    cleaned = re.sub(r"<li[^>]*>(.*?)</li>", r"\n* \1", cleaned, flags=re.IGNORECASE | re.DOTALL)
    # Convert paragraphs and breaks
    cleaned = re.sub(r"<p[^>]*>(.*?)</p>", r"\n\1\n", cleaned, flags=re.IGNORECASE | re.DOTALL)
    cleaned = re.sub(r"<br\s*/?>", "\n", cleaned, flags=re.IGNORECASE)
    # Remove remaining tags
    cleaned = re.sub(r"<[^>]+>", " ", cleaned)
    # Unescape HTML entities
    cleaned = (
        cleaned.replace("&amp;", "&")
        .replace("&lt;", "<")
        .replace("&gt;", ">")
        .replace("&quot;", '"')
        .replace("&#39;", "'")
        .replace("&nbsp;", " ")
    )
    # Collapse multiple whitespaces/newlines
    lines = [re.sub(r"[ \t]+", " ", line).strip() for line in cleaned.splitlines()]
    non_empty = [line for line in lines if line]
    return "\n\n".join(non_empty)


def _fetch_url(url: str, timeout: int = 10) -> str:
    """Fetch URL contents safely."""
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            content_type = resp.headers.get("Content-Type", "")
            if "text/html" not in content_type and "text/plain" not in content_type:
                return ""
            raw = resp.read()
            return raw.decode("utf-8", errors="replace")
    except Exception:
        return ""


def search_web(query: str, max_results: int = 4) -> tuple[bool, str]:
    """Execute search query and extract web page content summaries."""
    if not query.strip():
        return False, "search query cannot be empty"

    encoded_query = urllib.parse.quote_plus(query.strip())
    search_url = f"https://html.duckduckgo.com/html/?q={encoded_query}"

    html = _fetch_url(search_url, timeout=12)
    if not html:
        return False, f"unable to reach search gateway for query: {query}"

    # Extract DuckDuckGo result links and snippets
    # Pattern looks for <a class="result__url" href="..."> or <a class="result__snippet" ...>
    results: list[dict[str, str]] = []
    matches = re.findall(
        r'<a class="result__snippet[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>',
        html,
        flags=re.DOTALL | re.IGNORECASE,
    )

    if not matches:
        # Fallback regex for standard result links
        matches = re.findall(
            r'<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>',
            html,
            flags=re.DOTALL | re.IGNORECASE,
        )
        if matches:
            matches = [("", m) for m in matches]

    # Parse and compile summary
    findings: list[str] = [f"### Web Search Results for: {query}\n"]

    for idx, match in enumerate(matches[:max_results], 1):
        url, snippet_raw = match if isinstance(match, tuple) else ("", match)
        snippet = _clean_html_to_markdown(snippet_raw).strip()
        
        # Resolve DuckDuckGo redirect URL if present (e.g. //duckduckgo.com/l/?uddg=http%3A%2F%2F...)
        real_url = url
        if "uddg=" in url:
            parsed = urllib.parse.parse_qs(urllib.parse.urlparse(url).query)
            if "uddg" in parsed:
                real_url = parsed["uddg"][0]

        if not real_url.startswith("http"):
            real_url = f"Result #{idx}"

        findings.append(f"**[{idx}] {real_url}**\n{snippet}\n")

    if len(findings) == 1:
        return True, f"Search executed for {query!r} but returned 0 results."

    output = "\n".join(findings)
    if len(output) > 4000:
        output = output[:4000] + "\n...[truncated]..."

    return True, output
