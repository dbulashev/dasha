SELECT
    pid,
    CASE phase
        WHEN 'initializing' THEN 'Инициализация процесса базового копирования'
        WHEN 'waiting for checkpoint to finish' THEN 'Ожидание завершения контрольной точки'
        WHEN 'estimating backup size' THEN 'Оценка размера резервной копии'
        WHEN 'streaming database files' THEN 'Потоковая передача файлов базы данных'
        WHEN 'waiting for wal archiving to finish' THEN 'Ожидание завершения архивации WAL'
        WHEN 'transferring wal files' THEN 'Передача WAL файлов'
        WHEN 'finalizing' THEN 'Завершение процесса копирования'
        ELSE phase
        END AS phase_description,
    backup_total,
    backup_streamed,
    CASE WHEN backup_total > 0
        THEN ROUND((backup_streamed::numeric / backup_total::numeric) * 100, 2)
        ELSE NULL
        END AS progress_percentage,
    tablespaces_total,
    tablespaces_streamed
FROM
    pg_stat_progress_basebackup;
