SELECT
    pid,
    datname,
    relid::regclass::text AS table_name,
    command,
    CASE phase
        WHEN 'initializing' THEN 'Инициализация операции. Подготовка к началу работы.'
        WHEN 'seq scanning heap' THEN 'Последовательное сканирование кучи (исходной таблицы).'
        WHEN 'index scanning heap' THEN 'Сканирование кучи через индекс (для CLUSTER).'
        WHEN 'sorting tuples' THEN 'Сортировка кортежей перед записью.'
        WHEN 'writing new heap' THEN 'Запись новой кучи с упорядоченными данными.'
        WHEN 'swapping relation files' THEN 'Замена файлов исходной и новой таблицы.'
        WHEN 'rebuilding index' THEN 'Перестроение индексов.'
        WHEN 'performing final cleanup' THEN 'performing final cleanup - Завершающая очистка временных данных.'
        ELSE phase
        END AS phase_description,
    cluster_index_relid::regclass::text AS cluster_index,
    heap_tuples_scanned,
    heap_tuples_written,
    heap_blks_total,
    heap_blks_scanned,
    index_rebuild_count
FROM
    pg_stat_progress_cluster;
