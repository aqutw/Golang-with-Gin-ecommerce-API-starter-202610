package catalog

import (
	"context"
	domain "example.com/ecommerce/commerce/domain/catalog"
	"example.com/ecommerce/commerce/ports"
)

type Service struct{ Store ports.Store }

func (s Service) List(ctx context.Context) ([]domain.Product, error) {
	products := make([]domain.Product, 0)
	err := s.Store.View(ctx, func(r ports.Reader) error {
		all, err := r.Products()
		if err != nil {
			return err
		}
		for _, p := range all {
			if p.Active {
				products = append(products, p)
			}
		}
		return nil
	})
	return products, err
}
