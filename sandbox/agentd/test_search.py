"""Unit tests for Deep Web Search & Research Engine."""

import unittest
from unittest.mock import patch
import search


class TestDeepSearch(unittest.TestCase):
    def test_clean_html_to_markdown(self):
        sample_html = """
        <html>
        <head><title>Test Page</title><style>.foo { color: red; }</style></head>
        <body>
            <h1>Main Title</h1>
            <p>This is a paragraph with <b>bold text</b> and <a href="http://example.com">a link</a>.</p>
            <ul>
                <li>First bullet</li>
                <li>Second bullet</li>
            </ul>
            <script>alert("bad");</script>
        </body>
        </html>
        """
        md = search._clean_html_to_markdown(sample_html)
        self.assertIn("### Main Title", md)
        self.assertIn("This is a paragraph with bold text and a link", md)
        self.assertIn("* First bullet", md)
        self.assertIn("* Second bullet", md)
        self.assertNotIn("<script>", md)
        self.assertNotIn("alert", md)

    def test_empty_query(self):
        ok, res = search.search_web("")
        self.assertFalse(ok)
        self.assertIn("cannot be empty", res)

    @patch.object(search, "_fetch_url")
    def test_search_web_mocked(self, mock_fetch):
        mock_html = """
        <div class="results">
            <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org">Go is an open source programming language.</a>
            <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fpython.org">Python is powerful and fast.</a>
        </div>
        """
        mock_fetch.return_value = mock_html

        ok, summary = search.search_web("golang vs python", max_results=2)
        self.assertTrue(ok)
        self.assertIn("### Web Search Results for: golang vs python", summary)
        self.assertIn("https://golang.org", summary)
        self.assertIn("Go is an open source programming language", summary)
        self.assertIn("https://python.org", summary)


if __name__ == "__main__":
    unittest.main()
