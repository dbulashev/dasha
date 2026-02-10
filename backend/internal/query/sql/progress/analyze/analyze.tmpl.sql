SELECT
    pid,
    datname,
    relid::regclass::text AS table_name,
    CASE phase
        WHEN 'initializing' THEN 'Команда готовится начать сканирование кучи. Эта фаза должна быть очень быстрой'
        WHEN 'acquiring sample rows' THEN 'Сканирование таблицы для сбора строк выборки'
        WHEN 'acquiring inherited sample rows' THEN 'Сканирование таблиц-потомков для сбора строк выборки (для наследуемых таблиц)'
        WHEN 'computing statistics' THEN 'Вычисление статистики на основе собранных строк выборки'
        WHEN 'computing extended statistics' THEN 'Вычисление расширенной статистики'
        WHEN 'finalizing analyze' THEN 'Завершение анализа и сохранение результатов'
        ELSE phase
        END AS phase_description,
    sample_blks_total,
    sample_blks_scanned,
    ext_stats_total,
    ext_stats_computed,
    current_child_table_relid::regclass AS current_child_table
FROM
    pg_stat_progress_analyze;
