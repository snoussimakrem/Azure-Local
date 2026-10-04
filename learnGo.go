import ( "" , )

type Config struct {
	DataDir string 
	Gateway string
	Debug bool }

	type service interface {
	Start(ctx context.context.Context) error
	stop




}