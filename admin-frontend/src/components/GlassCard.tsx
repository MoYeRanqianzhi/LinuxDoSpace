import type { PropsWithChildren } from 'react';
import { motion } from 'motion/react';

// GlassCard 负责复用管理员端的玻璃拟态卡片容器。
export function GlassCard({ children, className = '' }: PropsWithChildren<{ className?: string }>) {
  return (
    <motion.section
      initial={{ opacity: 0, y: 18 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.35 }}
      className={`admin-grid-shadow rounded-[28px] border border-white/30 bg-white/45 p-6 backdrop-blur-xl dark:border-white/10 dark:bg-black/35 ${className}`}
    >
      {children}
    </motion.section>
  );
}
