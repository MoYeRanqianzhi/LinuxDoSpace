import type { PropsWithChildren, SelectHTMLAttributes } from 'react';
import { ChevronDown } from 'lucide-react';

// AdminSelectProps extends the native select contract so every existing admin
// form can migrate without changing its state-management logic.
interface AdminSelectProps extends PropsWithChildren<SelectHTMLAttributes<HTMLSelectElement>> {
  wrapperClassName?: string;
}

// AdminSelect keeps the native browser select behavior but applies the same
// glass-style visual treatment across the entire admin frontend.
export function AdminSelect({ children, className = '', wrapperClassName = '', ...props }: AdminSelectProps) {
  return (
    <div className={`relative w-full ${wrapperClassName}`.trim()}>
      <select
        {...props}
        className={[
          'w-full appearance-none rounded-2xl border border-slate-200 bg-white/65 px-4 py-3 pr-11 outline-none transition',
          'focus:ring-2 dark:border-slate-700 dark:bg-black/35 dark:text-white',
          'disabled:cursor-not-allowed disabled:bg-slate-100 disabled:text-slate-500 dark:disabled:bg-slate-800 dark:disabled:text-slate-400',
          className,
        ].join(' ')}
      >
        {children}
      </select>

      <div className="pointer-events-none absolute inset-y-0 right-4 flex items-center text-slate-400 dark:text-slate-500">
        <ChevronDown size={18} />
      </div>
    </div>
  );
}
