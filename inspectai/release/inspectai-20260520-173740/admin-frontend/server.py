import http.server
import os
from pathlib import Path


class NoCacheHandler(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Cache-Control", "no-store, must-revalidate")
        super().end_headers()


def main():
    root = Path(__file__).resolve().parent
    os.chdir(root)
    host = os.environ.get("ADMIN_FRONTEND_HOST", "127.0.0.1")
    port = int(os.environ.get("ADMIN_FRONTEND_PORT", "18081"))
    server = http.server.ThreadingHTTPServer((host, port), NoCacheHandler)
    print(f"Admin frontend listening on http://{host}:{port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
