import WorldGallery from "./world-gallery";
import { SiteHeader } from "./lib/site-header";
import { MobileNav } from "./lib/mobile-nav";

export default function Home() {
  return (
    <div className="site-shell">
      <SiteHeader />

      <main className="page-container">
        <header className="page-heading">
          <h1>世界广场</h1>
        </header>
        <WorldGallery />
      </main>

      <MobileNav />
    </div>
  );
}
