-- A sealed provider draft can be saved before the supplier's prices are
-- established. It cannot reserve or submit calls until explicitly verified.
ALTER TABLE ai_model_rates DROP CONSTRAINT ai_model_rates_check;
ALTER TABLE ai_model_rates ADD CONSTRAINT ai_model_rates_verified_price
    CHECK(NOT verified OR input_rate+output_rate>0);
