import { useEffect, useState } from 'react';
import { motion } from 'motion/react';
import { Eye, ExternalLink, LoaderCircle, ShieldAlert } from 'lucide-react';
import { GlassCard } from '../components/GlassCard';
import { APIError, listPublicSupervisionEntries } from '../lib/api';
import type { SupervisionEntry } from '../types/api';

// Supervision 负责展示可公开监督的子域归属列表。
// 这里刻意只读取脱敏归属信息，不展示任何 DNS 解析值。
export function Supervision() {
  // entries 保存后端返回的全部公开监督记录。
  const [entries, setEntries] = useState<SupervisionEntry[]>([]);

  // loading 用于控制首次进入页面时的读取态。
  const [loading, setLoading] = useState(true);

  // error 用于保存公开监督列表加载失败时的提示信息。
  const [error, setError] = useState('');

  // 页面首次挂载时拉取公开监督数据。
  useEffect(() => {
    void loadEntries();
  }, []);

  // loadEntries 从后端读取全部可公开监督的子域归属列表。
  async function loadEntries(): Promise<void> {
    setLoading(true);
    setError('');

    try {
      const nextEntries = await listPublicSupervisionEntries();
      setEntries(nextEntries);
    } catch (loadError) {
      setEntries([]);
      setError(readableErrorMessage(loadError, '无法加载公开监督列表'));
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="max-w-6xl mx-auto pt-32 pb-24 px-6">
      <motion.div
        initial={{ y: 20, opacity: 0 }}
        animate={{ y: 0, opacity: 1 }}
        className="mb-8 flex flex-col gap-5"
      >
        <div className="text-center">
          <div className="inline-flex items-center gap-2 px-4 py-2 rounded-full bg-white/35 dark:bg-black/30 border border-white/30 dark:border-white/10 text-teal-700 dark:text-teal-300 text-sm font-semibold backdrop-blur-md">
            <Eye size={16} />
            共同监督
          </div>
          <h1 className="mt-5 text-4xl md:text-5xl font-extrabold text-gray-900 dark:text-white">
            一起守住社区子域名的边界
          </h1>
          <p className="mt-4 text-lg text-gray-700 dark:text-gray-200 max-w-4xl mx-auto leading-relaxed">
            共同监督，若你发现恶意、违法、滥用或明显不合理使用的子域名，向站长举报并经核实后，可获得免费子域名奖励。
          </p>
        </div>

        <GlassCard className="p-5">
          <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
            <div className="flex items-start gap-3">
              <div className="mt-0.5 p-2 rounded-2xl bg-amber-100/70 dark:bg-amber-950/35 text-amber-700 dark:text-amber-300">
                <ShieldAlert size={18} />
              </div>
              <div>
                <div className="text-base font-bold text-gray-900 dark:text-white">本页只公开归属，不公开解析值</div>
                <div className="mt-1 text-sm text-gray-600 dark:text-gray-300">
                  你在这里看到的只有子域名和拥有者信息。任何具体 IP、CNAME、上游服务地址等敏感解析数据都不会显示。
                </div>
              </div>
            </div>

            <div className="rounded-2xl bg-white/35 dark:bg-black/30 border border-white/20 px-4 py-3 text-sm text-gray-700 dark:text-gray-200">
              当前公开条目：{entries.length}
            </div>
          </div>
        </GlassCard>

        {error && (
          <div className="rounded-2xl border border-red-300/40 bg-red-100/60 dark:bg-red-950/25 dark:border-red-700/40 px-4 py-3 text-sm text-red-900 dark:text-red-200">
            {error}
          </div>
        )}
      </motion.div>

      <GlassCard className="overflow-hidden p-0">
        <div className="overflow-x-auto">
          <table className="w-full text-left border-collapse">
            <thead>
              <tr className="border-b border-white/20 dark:border-white/10 bg-white/20 dark:bg-black/20">
                <th className="p-4 font-semibold text-gray-900 dark:text-white">子域名</th>
                <th className="p-4 font-semibold text-gray-900 dark:text-white">拥有者</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((entry) => (
                <tr
                  key={`${entry.fqdn}:${entry.owner_username}`}
                  className="border-b border-white/10 dark:border-white/5 hover:bg-white/30 dark:hover:bg-white/5 transition-colors"
                >
                  <td className="p-4">
                    <a
                      href={`https://${entry.fqdn}`}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-2 font-semibold text-teal-700 dark:text-teal-300 hover:text-teal-800 dark:hover:text-teal-200"
                    >
                      <span>{entry.fqdn}</span>
                      <ExternalLink size={15} />
                    </a>
                  </td>
                  <td className="p-4 text-gray-700 dark:text-gray-200">
                    <div className="font-semibold">{entry.owner_display_name || entry.owner_username}</div>
                    <div className="text-sm text-gray-500 dark:text-gray-400">@{entry.owner_username}</div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {loading && (
          <div className="p-12 text-center text-gray-500 dark:text-gray-400 flex items-center justify-center gap-3">
            <LoaderCircle size={18} className="animate-spin" />
            正在加载公开监督列表...
          </div>
        )}

        {!loading && entries.length === 0 && !error && (
          <div className="p-12 text-center text-gray-500 dark:text-gray-400">
            当前还没有可公开展示的子域归属记录。
          </div>
        )}
      </GlassCard>
    </div>
  );
}

// readableErrorMessage 把浏览器端异常统一整理为更直观的页面提示。
function readableErrorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    return error.message;
  }
  if (error instanceof Error && error.message.trim() !== '') {
    return error.message;
  }
  return fallback;
}
