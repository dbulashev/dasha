SELECT
    pid,
    datname,
    relid::regclass AS table_name,
    index_relid::regclass AS index_name,
    CASE phase
        WHEN 'initializing' THEN 'Инициализация операции создания индекса'
        WHEN 'waiting for writers before build' THEN 'Ожидание завершения операций записи перед построением'
        WHEN 'building index' THEN 'Построение индекса (основная фаза)'
        WHEN 'waiting for writers before validation' THEN 'Ожидание завершения операций записи перед валидацией'
        WHEN 'index validation' THEN 'Проверка целостности индекса'
        WHEN 'waiting for old snapshots' THEN 'Ожидание завершения старых транзакций'
        WHEN 'waiting for readers before marking dead' THEN 'Ожидание читателей перед пометкой старого индекса как неактивного'
        WHEN 'waiting for readers before dropping' THEN 'Ожидание читателей перед удалением старого индекса'
        ELSE phase
        END AS phase_description,
    lockers_total,
    lockers_done,
    current_locker_pid,
    blocks_total,
    blocks_done,
    tuples_total,
    tuples_done,
    partitions_total,
    partitions_done
FROM
    pg_stat_progress_create_index;