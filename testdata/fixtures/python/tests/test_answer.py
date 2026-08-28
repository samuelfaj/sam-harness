from fixture_python import answer


def test_answer_explains_the_fixture_contract() -> None:
    assert answer() == 42
