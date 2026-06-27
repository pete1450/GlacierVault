import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: 'export',   // static HTML/JS/CSS — served by Go's http.FileServer
  trailingSlash: true,
};

export default nextConfig;
