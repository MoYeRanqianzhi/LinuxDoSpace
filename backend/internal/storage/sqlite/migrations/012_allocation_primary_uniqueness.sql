-- 012_allocation_primary_uniqueness.sql repairs any historical duplicate
-- primary allocations and then lets SQLite enforce the invariant permanently
-- with one partial unique index.

-- 历史竞态可能留下同一用户、同一根域名下多条 `is_primary = 1` 的脏数据。
-- 这里按最近更新时间、创建时间、主键倒序保留一条，其余全部降级为非主分配。
WITH ranked_primary_allocations AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY user_id, managed_domain_id
            ORDER BY updated_at DESC, created_at DESC, id DESC
        ) AS primary_rank
    FROM allocations
    WHERE is_primary = 1
)
UPDATE allocations
SET is_primary = 0
WHERE id IN (
    SELECT id
    FROM ranked_primary_allocations
    WHERE primary_rank > 1
);

-- 数据库层面的最终兜底：任何写路径都不能再为同一用户、同一根域名保留两条主分配。
CREATE UNIQUE INDEX IF NOT EXISTS idx_allocations_primary_user_domain
ON allocations(user_id, managed_domain_id)
WHERE is_primary = 1;
