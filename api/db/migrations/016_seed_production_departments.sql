DO $$
DECLARE
    algorithm_department_id UUID;
    test_department_id UUID;
BEGIN
    SELECT d.id INTO algorithm_department_id
    FROM departments d
    JOIN users u ON u.id = d.director_user_id
    WHERE u.username = '965';

    SELECT d.id INTO test_department_id
    FROM departments d
    JOIN users u ON u.id = d.director_user_id
    WHERE u.username = 't10';

    -- This seed is production-specific. Other environments keep their configured departments.
    IF algorithm_department_id IS NULL AND test_department_id IS NULL THEN
        RETURN;
    END IF;
    IF algorithm_department_id IS NULL OR test_department_id IS NULL THEN
        RAISE EXCEPTION 'production department seed requires directors 965 and t10';
    END IF;

    UPDATE departments
    SET name = CASE id
        WHEN algorithm_department_id THEN '算法产品部'
        WHEN test_department_id THEN '测试部'
        ELSE name
    END,
    updated_at = now()
    WHERE id IN (algorithm_department_id, test_department_id);

    UPDATE teams
    SET department_id = CASE director_user_id
        WHEN (SELECT id FROM users WHERE username = 't10') THEN test_department_id
        ELSE algorithm_department_id
    END;

    UPDATE users
    SET department_id = CASE
        WHEN username ~ '^t(0[1-9]|10)$' THEN test_department_id
        ELSE algorithm_department_id
    END;
END $$;
