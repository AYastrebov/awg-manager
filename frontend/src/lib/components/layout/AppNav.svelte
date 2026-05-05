<script lang="ts">
  import { page } from '$app/stores';
  import { Icon } from '$lib/components/ui';
  import { systemInfo } from '$lib/stores/system';
  import { serverOnline } from '$lib/stores/events';
  import { goto } from '$app/navigation';

  type NavItem = {
    id: string;
    label: string;
    icon: string;
    href: string;
    matches: (path: string) => boolean;
  };

  const NAV_ITEMS: NavItem[] = [
    { id: 'tunnels',     label: 'Туннели',       icon: 'plug',         href: '/',            matches: (p) => p === '/' || p.startsWith('/tunnels') || p.startsWith('/system-tunnels') || p.startsWith('/subscriptions') },
    { id: 'servers',     label: 'Серверы',       icon: 'git-branch',   href: '/servers',     matches: (p) => p.startsWith('/servers') },
    { id: 'routing',     label: 'Маршрутизация', icon: 'git-branch',   href: '/routing',     matches: (p) => p.startsWith('/routing') || p.startsWith('/singbox') },
    { id: 'monitoring',  label: 'Мониторинг',    icon: 'activity',     href: '/monitoring',  matches: (p) => p.startsWith('/monitoring') || p.startsWith('/pingcheck') || p.startsWith('/connections') },
    { id: 'diagnostics', label: 'Диагностика',   icon: 'stethoscope',  href: '/diagnostics', matches: (p) => p.startsWith('/diagnostics') },
    { id: 'logs',        label: 'Журнал',        icon: 'file-text',    href: '/logs',        matches: (p) => p.startsWith('/logs') },
    { id: 'terminal',    label: 'Терминал',      icon: 'terminal-2',   href: '/terminal',    matches: (p) => p.startsWith('/terminal') },
    { id: 'settings',    label: 'Настройки',     icon: 'settings',     href: '/settings',    matches: (p) => p.startsWith('/settings') },
  ];

  interface Props {
    currentVersion?: string;
    onBellClick?: () => void;
    bellCount?: number;
  }

  let { currentVersion = '', onBellClick, bellCount = 0 }: Props = $props();

  let sysInfo = $derived($systemInfo.data);
  let online = $derived($serverOnline);
  let routerLabel = $derived.by(() => {
    if (!sysInfo) return '';
    const model = sysInfo.kernelModuleModel || sysInfo.routerIP || 'router';
    return `${model} · ${sysInfo.goArch}`;
  });

  let activeId = $derived(NAV_ITEMS.find((it) => it.matches($page.url.pathname))?.id ?? 'tunnels');

  function handleNav(e: MouseEvent, item: NavItem) {
    if (e.metaKey || e.ctrlKey || e.button !== 0) return;
    e.preventDefault();
    goto(item.href);
  }
</script>

<header class="appnav">
  <div class="brand">
    <div class="logo" aria-hidden="true">
      <span class="bar bar-a"></span>
      <span class="bar bar-b"></span>
      <span class="bar bar-c"></span>
    </div>
    <span class="wordmark">awg<span class="dot">·</span>manager</span>
    {#if currentVersion}
      <span class="version">{currentVersion}</span>
    {/if}
  </div>

  <nav class="nav" aria-label="Главная навигация">
    {#each NAV_ITEMS as item (item.id)}
      <a
        class="link"
        class:active={item.id === activeId}
        href={item.href}
        onclick={(e) => handleNav(e, item)}
      >
        <Icon name={item.icon} size={14} color={item.id === activeId ? 'var(--color-canvas)' : 'var(--color-text-secondary)'} />
        {item.label}
      </a>
    {/each}
  </nav>

  <div class="right">
    {#if sysInfo}
      <span class="router">
        <span class="led" data-online={online ? 'true' : 'false'}></span>
        {routerLabel}
      </span>
    {/if}
    <button type="button" class="bell" onclick={onBellClick} aria-label="Уведомления">
      <Icon name="bell" size={12} />
      {#if bellCount > 0}<span class="bell-count">{bellCount}</span>{/if}
    </button>
  </div>
</header>

<style>
  .appnav {
    height: 56px;
    background: var(--color-canvas);
    border-bottom: 1px solid var(--color-border);
    display: flex;
    align-items: center;
    padding: 0 24px;
    gap: 32px;
  }
  .brand { display: flex; align-items: center; gap: 10px; }
  .logo {
    display: grid;
    grid-template-columns: 5px 5px 5px;
    gap: 2px;
    align-items: end;
    width: 24px;
    height: 24px;
  }
  .bar { display: block; }
  .bar-a { height: 10px; background: var(--color-text-secondary); }
  .bar-b { height: 16px; background: var(--color-yellow); }
  .bar-c { height: 7px;  background: var(--color-text-secondary); }
  .wordmark {
    font: 700 15px/1 var(--font-sans);
    letter-spacing: -0.2px;
    color: var(--color-text-primary);
  }
  .dot { color: var(--color-yellow); }
  .version {
    font: 500 11px/1 var(--font-mono);
    color: var(--color-text-muted);
    margin-left: 8px;
  }

  .nav { display: flex; gap: 4px; flex: 1; flex-wrap: wrap; }
  .link {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    font: 500 13px/1 var(--font-sans);
    color: var(--color-text-secondary);
    background: transparent;
    border-radius: 6px;
    text-decoration: none;
    transition: background 120ms ease, color 120ms ease;
    cursor: pointer;
  }
  .link:hover { background: var(--color-bg-tertiary); color: var(--color-text-primary); }
  .link.active { background: var(--color-yellow); color: var(--color-canvas); }

  .right { display: flex; align-items: center; gap: 12px; }
  .router {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font: 500 12px/1 var(--font-mono);
    color: var(--color-text-muted);
  }
  .led { width: 6px; height: 6px; border-radius: 9999px; background: var(--color-success); }
  .led[data-online='false'] { background: var(--color-error); }

  .bell {
    height: 32px;
    padding: 0 12px;
    background: transparent;
    color: var(--color-text-secondary);
    border: 1px solid var(--color-border-hover);
    border-radius: 6px;
    font: 500 12px/1 var(--font-sans);
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .bell:hover { color: var(--color-text-primary); }
  .bell-count { font-family: var(--font-mono); }
</style>
