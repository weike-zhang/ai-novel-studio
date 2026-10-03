import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  output: "standalone",
  images: {
    remotePatterns: [
      { protocol: "http", hostname: "media.example.invalid", port: "9000" },
    ],
  },
  async rewrites() {
    const target = process.env.API_PROXY_TARGET?.replace(/\/$/, "");
    return target ? [{ source: "/v1/:path*", destination: `${target}/v1/:path*` }] : [];
  },
};

export default nextConfig;
