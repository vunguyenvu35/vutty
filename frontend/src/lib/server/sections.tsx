import type {ReactNode} from 'react'

export type Section = 'overview' | 'incident' | 'processes' | 'services' | 'docker' | 'kubernetes' | 'logs' | 'ports' | 'web' | 'maintenance' | 'security' | 'schedule' | 'users'

/** The sections of the panel by what they are for: look at the server, run what is on it, look after it. */
export const SECTION_GROUPS: {id: 'monitor' | 'run' | 'manage'; sections: Section[]}[] = [
  {id: 'monitor', sections: ['overview', 'processes', 'logs', 'incident']},
  {id: 'run', sections: ['services', 'docker', 'kubernetes', 'web', 'schedule']},
  {id: 'manage', sections: ['ports', 'security', 'users', 'maintenance']},
]

const I = (d: ReactNode) => <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{d}</svg>

export const SECTION_ICONS: Record<Section, ReactNode> = {
  overview: I(<><rect x="3" y="3" width="7" height="9" rx="1.5" /><rect x="14" y="3" width="7" height="5" rx="1.5" /><rect x="14" y="12" width="7" height="9" rx="1.5" /><rect x="3" y="16" width="7" height="5" rx="1.5" /></>),
  processes: I(<path d="M4 6h16M4 12h16M4 18h10" />),
  logs: I(<><path d="M5 4h11l3 3v13H5z" /><path d="M8 11h8M8 15h8" /></>),
  incident: I(<><path d="M12 3 2.5 20h19z" /><path d="M12 10v4M12 17.2v.1" /></>),
  services: I(<><circle cx="12" cy="12" r="3" /><path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M18.4 5.6l-2.1 2.1M7.7 16.3l-2.1 2.1" /></>),
  docker: I(<><path d="M3 13h18c0 4-3 7-8 7s-10-2-10-7z" /><path d="M6 13V9h3v4M10 13V9h3v4M10 9V6h3v3M14 13V9h3v4" /></>),
  kubernetes: I(<><path d="M12 2.5 20 7v10l-8 4.5L4 17V7z" /><circle cx="12" cy="12" r="2.5" /></>),
  web: I(<><circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18" /></>),
  schedule: I(<><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>),
  ports: I(<path d="M12 3 4 6v6c0 4.5 3.3 7.8 8 9 4.7-1.2 8-4.5 8-9V6z" />),
  security: I(<><rect x="5" y="11" width="14" height="10" rx="2" /><path d="M8 11V8a4 4 0 0 1 8 0v3" /></>),
  users: I(<><circle cx="9" cy="8" r="3.5" /><path d="M2.5 20c.8-3.5 3.3-5.5 6.5-5.5s5.7 2 6.5 5.5M16 5a3.5 3.5 0 0 1 0 6.5M18 14.8c1.7.7 3 2.4 3.5 5" /></>),
  maintenance: I(<path d="M14.7 6.3a4 4 0 0 0-5.4 5.1L3 17.7 6.3 21l6.3-6.3a4 4 0 0 0 5.1-5.4l-2.6 2.6-2.4-.6-.6-2.4z" />),
}
