import { useCallback, useEffect, useMemo, useState } from 'react';

type Theme = 'light' | 'dark';

const storageKey = 'brancr_admin_theme';

function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
}

export function useTheme() {
  const [theme, setTheme] = useState<Theme>('light');

  useEffect(() => {
    const saved = localStorage.getItem(storageKey) as Theme | null;
    const initial = saved === 'dark' ? 'dark' : 'light';
    setTheme(initial);
    applyTheme(initial);
  }, []);

  const toggleTheme = useCallback(() => {
    setTheme((t) => {
      const next: Theme = t === 'dark' ? 'light' : 'dark';
      localStorage.setItem(storageKey, next);
      applyTheme(next);
      return next;
    });
  }, []);

  return useMemo(() => ({ theme, toggleTheme }), [theme, toggleTheme]);
}
