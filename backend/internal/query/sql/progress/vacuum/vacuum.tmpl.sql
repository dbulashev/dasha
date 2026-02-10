SELECT
    pid,
    datname,
    relid::regclass AS table_name,
    CASE phase
        WHEN 'initializing' THEN 'Инициализация операции VACUUM'
        WHEN 'scanning heap' THEN 'Сканирование кучи таблицы'
        WHEN 'vacuuming indexes' THEN 'Очистка индексов таблицы'
        WHEN 'vacuuming heap' THEN 'Очистка основной кучи таблицы'
        WHEN 'cleaning up indexes' THEN 'Финализация очистки индексов'
        WHEN 'truncating heap' THEN 'Усечение пустого пространства в конце таблицы'
        WHEN 'performing final cleanup' THEN 'Завершающая очистка'
        ELSE phase
        END AS phase_description,
    heap_blks_total,
    heap_blks_scanned,
    heap_blks_vacuumed,
    index_vacuum_count,
    max_dead_tuple_bytes as max_dead_tuples,
    dead_tuple_bytes as num_dead_tuples
FROM
    pg_stat_progress_vacuum;
