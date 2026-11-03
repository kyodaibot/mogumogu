// The sources (GLOSSARY: 来源) shown as links under a reply.

import type { Source } from '../api'

export interface SourceLink {
  href: string
  label: string
}

/**
 * The links to show: http and https only (Go checks this too, but a link
 * reaching the page must never be javascript: or the like), labelled with the
 * page title, or its domain when the title is empty.
 */
export function sourceLinks(sources: Source[]): SourceLink[] {
  const links: SourceLink[] = []
  for (const s of sources) {
    let url: URL
    try {
      url = new URL(s.url)
    } catch {
      continue
    }
    if (url.protocol !== 'http:' && url.protocol !== 'https:') continue
    links.push({ href: url.href, label: s.title.trim() || url.hostname.replace(/^www\./, '') })
  }
  return links
}
